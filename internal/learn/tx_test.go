// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package learn

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/state"
)

// failing is a proven shape that fails while it runs, after it's planned.
const failing = "SELECT current_setting('enable_nestloop') AS v WHERE 1 / (SELECT 0) = 1"

func provenProbeAndFailing(f *state.File, db string) {
	provenProbe(f, db)
	key := db + "/" + sqltext.Fingerprint(failing)
	f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(failing), Database: db, Status: Proven,
		Proven: "no nested loop", Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}}, Arms: map[string]*state.Arm{}}
}

func (r *activeRig) exec(q string) {
	r.t.Helper()
	if _, err := r.app.Exec(context.Background(), q); err != nil {
		r.t.Fatalf("%s: %v", q, err)
	}
}

// settings shows what a statement in the session sees now.
func (r *activeRig) settings() string {
	r.t.Helper()
	return r.simple("SELECT current_setting('enable_nestloop') || ' ' || current_setting('plan_cache_mode') || ' ' || current_setting('work_mem')")
}

// Inside the application's transaction the hinted statement sees the hint,
// the statements after it don't, and the application's own settings in
// that transaction come back as they were, whether it set them with SET
// LOCAL or SET.
func TestHintInsideTransaction(t *testing.T) {
	r := newActiveRig(t, "", provenProbe)
	before := r.settings()
	for _, run := range []struct {
		name string
		f    func(string) string
	}{{"simple", r.simple}, {"extended", r.extended}} {
		r.exec("BEGIN")
		r.exec("SET LOCAL plan_cache_mode = force_generic_plan")
		r.exec("SET work_mem = '7MB'")
		for i := 0; i < 3; i++ {
			if v := run.f(probe); v != "off" {
				t.Errorf("%s: call %d inside the transaction: enable_nestloop = %s", run.name, i, v)
			}
			if got := r.settings(); got != "on force_generic_plan 7MB" {
				t.Errorf("%s: after call %d: %q", run.name, i, got)
			}
		}
		r.exec("COMMIT")
		if got := r.settings(); got != strings.Replace(before, before[strings.LastIndex(before, " ")+1:], "7MB", 1) {
			t.Errorf("%s: after COMMIT: %q", run.name, got)
		}
		r.exec("RESET work_mem")
	}
	// Two statements in one simple query inside a transaction: no hint.
	r.exec("BEGIN")
	res, err := r.app.Exec(context.Background(), probe+"; SELECT 1")
	if err != nil || *res[0].Rows[0][0] != "on" {
		t.Errorf("two statements in one query: %v %v", res, err)
	}
	r.exec("COMMIT")
	r.col.Flush()
	sh, _ := r.col.Get(r.db + "/" + sqltext.Fingerprint(probe))
	if sh.HintedCalls != 6 {
		t.Errorf("hinted calls %d, want 6", sh.HintedCalls)
	}
}

// Savepoints, and a hinted statement that fails: after ROLLBACK TO
// SAVEPOINT or ROLLBACK, nothing of LQO's is left in the session.
func TestHintWithSavepointsAndErrors(t *testing.T) {
	r := newActiveRig(t, "", provenProbeAndFailing)
	ctx := context.Background()
	for _, ext := range []bool{false, true} {
		run := r.simple
		if ext {
			run = r.extended
		}
		r.exec("BEGIN")
		r.exec("SAVEPOINT a")
		if v := run(probe); v != "off" {
			t.Errorf("extended=%v: in a savepoint: %s", ext, v)
		}
		r.exec("ROLLBACK TO SAVEPOINT a")
		if got := r.settings(); !strings.HasPrefix(got, "on auto") {
			t.Errorf("extended=%v: after ROLLBACK TO: %q", ext, got)
		}
		r.exec("RELEASE SAVEPOINT a")
		// A hinted statement fails: the transaction is aborted, and the
		// application rolls back to its savepoint, as ORMs do.
		r.exec("SAVEPOINT b")
		var err error
		if ext {
			_, err = r.app.ExecOne(ctx, failing)
		} else {
			_, err = r.app.Exec(ctx, failing)
		}
		if !pgwire.IsCode(err, "22012") {
			t.Fatalf("extended=%v: want division by zero, got %v", ext, err)
		}
		r.exec("ROLLBACK TO SAVEPOINT b")
		if got := r.settings(); !strings.HasPrefix(got, "on auto") {
			t.Errorf("extended=%v: after the error and ROLLBACK TO: %q", ext, got)
		}
		// And the hint still works in the same transaction.
		if v := run(probe); v != "off" {
			t.Errorf("extended=%v: after the savepoint: %s", ext, v)
		}
		if got := r.settings(); !strings.HasPrefix(got, "on auto") {
			t.Errorf("extended=%v: after the hint: %q", ext, got)
		}
		// Fails again, and this time the whole transaction is rolled back.
		if ext {
			_, err = r.app.ExecOne(ctx, failing)
		} else {
			_, err = r.app.Exec(ctx, failing)
		}
		var pe *pgwire.PgError
		if !errors.As(err, &pe) {
			t.Fatalf("extended=%v: %v", ext, err)
		}
		r.exec("ROLLBACK")
		if got := r.settings(); !strings.HasPrefix(got, "on auto") {
			t.Errorf("extended=%v: after ROLLBACK: %q", ext, got)
		}
		// A hint after the failure, outside and inside a transaction.
		if v := run(probe); v != "off" {
			t.Errorf("extended=%v: after the failures: %s", ext, v)
		}
		r.exec("BEGIN")
		if v := run(probe); v != "off" {
			t.Errorf("extended=%v: in a new transaction: %s", ext, v)
		}
		r.exec("COMMIT")
	}
}

// raw builds protocol messages for a pipeline.
type raw []byte

func (b raw) msg(typ byte, parts ...[]byte) raw {
	n := 4
	for _, p := range parts {
		n += len(p)
	}
	b = append(b, typ)
	b = binary.BigEndian.AppendUint32(b, uint32(n))
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

func cs(s string) []byte { return append([]byte(s), 0) }

// query adds Parse, Bind, Execute and Sync for q, unnamed.
func (b raw) query(q string) raw {
	return b.msg('P', cs(""), cs(q), []byte{0, 0}).
		msg('B', cs(""), cs(""), []byte{0, 0, 0, 0, 0, 0}).
		msg('D', []byte{'P'}, cs("")).
		msg('E', cs(""), []byte{0, 0, 0, 0}).
		msg('S')
}

// replies reads up to n ReadyForQuery messages and returns every message
// type seen, with the values of single-column rows.
func (r *activeRig) replies(n int) (types string, values []string) {
	r.t.Helper()
	for n > 0 {
		typ, body, err := r.app.ReceiveRaw()
		if err != nil {
			r.t.Fatal(err)
		}
		types += string(typ)
		switch typ {
		case 'D':
			if len(body) >= 6 && binary.BigEndian.Uint16(body) == 1 {
				l := int(int32(binary.BigEndian.Uint32(body[2:])))
				if l >= 0 && 6+l <= len(body) {
					values = append(values, string(body[6:6+l]))
				}
			}
		case 'Z':
			n--
		}
	}
	return types, values
}

// Pipelines inside a transaction: every reply the client gets is its own,
// in order, and no hint reaches a statement it wasn't for. The first
// statement of a pipeline sent while the session is idle is hinted; the
// rest go as they are while others are in flight.
func TestPipelineInsideTransaction(t *testing.T) {
	r := newActiveRig(t, "", provenProbeAndFailing)
	show := "SELECT current_setting('enable_nestloop') || current_setting('plan_cache_mode') AS s"
	r.exec("BEGIN")
	var b raw
	b = b.query(probe).query(show).query(probe).query(show).
		msg('P', cs(""), cs("SAVEPOINT p"), []byte{0, 0}).msg('B', cs(""), cs(""), []byte{0, 0, 0, 0, 0, 0}).msg('E', cs(""), []byte{0, 0, 0, 0}).msg('S').
		query(probe).query(show)
	if err := r.app.SendRaw(b); err != nil {
		t.Fatal(err)
	}
	types, values := r.replies(7)
	want := "1" + "2TDCZ"
	wantAll := strings.Repeat(want, 4) + "12CZ" + strings.Repeat(want, 2)
	if types != wantAll {
		t.Fatalf("replies %q\nwant    %q", types, wantAll)
	}
	if len(values) != 6 || values[0] != "off" {
		t.Fatalf("values %q", values)
	}
	for i := 1; i < 6; i += 2 {
		if values[i] != "onauto" {
			t.Errorf("statement %d of the pipeline saw %q", i+1, values[i])
		}
	}
	// A failure in the middle of a pipeline: the server skips to the Sync,
	// and the statements after it are answered as usual.
	r.exec("ROLLBACK")
	r.exec("BEGIN")
	b = raw(nil).query(failing).query(show)
	r.app.SendRaw(b)
	types, values = r.replies(2)
	if !strings.HasPrefix(types, "12TE") && !strings.HasPrefix(types, "1E") {
		t.Errorf("replies %q", types)
	}
	if !strings.HasSuffix(types, "Z") || strings.Count(types, "Z") != 2 || strings.Contains(types[strings.Index(types, "Z"):], "D") {
		t.Errorf("after the failure: %q %q", types, values)
	}
	r.exec("ROLLBACK")
	if got := r.settings(); !strings.HasPrefix(got, "on auto") {
		t.Errorf("after ROLLBACK: %q", got)
	}
	if v := r.extended(probe); v != "off" {
		t.Errorf("after the pipeline: %s", v)
	}
}
