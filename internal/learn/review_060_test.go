// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Review of 0.6.0 Phase A (hints inside the application's transactions).
// Each test here showed a defect found in review, and failed before the fix.

package learn

import (
	"context"
	"strings"
	"testing"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/state"
)

// The application (or a pooler in front of LQO, such as PgBouncer's
// server_reset_query = DISCARD ALL) drops every prepared statement in the
// session. LQO still thinks lqo_hint_N exists, so its Bind fails: the
// application's own statement fails, inside a transaction the
// transaction is aborted, and since the name is never forgotten, every
// hinted call in the session fails from then on.
func TestReviewDeallocateAllBreaksHintedCalls(t *testing.T) {
	r := newActiveRig(t, "", provenProbe)
	ctx := context.Background()
	if v := r.extended(probe); v != "off" {
		t.Fatalf("not hinted to start with: %s", v)
	}
	for _, reset := range []string{"DEALLOCATE ALL", "DISCARD ALL"} {
		r.exec(reset)
		if _, err := r.app.ExecOne(ctx, probe); err != nil {
			t.Errorf("after %s, outside a transaction: %v", reset, err)
		}
		r.exec("BEGIN")
		if _, err := r.app.ExecOne(ctx, probe); err != nil {
			t.Errorf("after %s, inside a transaction: %v", reset, err)
		}
		if _, err := r.app.ExecOne(ctx, "SELECT 1"); err != nil {
			t.Errorf("after %s: the application's transaction was aborted: %v", reset, err)
		}
		r.app.Exec(ctx, "ROLLBACK")
		// And again: a later hinted call works once more.
		if _, err := r.app.ExecOne(ctx, probe); err != nil {
			t.Errorf("after %s, the session stays broken: %v", reset, err)
		}
	}
}

// An application cursor (or portal) named lqo_hint: LQO's Bind to its
// fixed portal name collides with it, and the application's statement
// fails. Inside a transaction a cursor of that name is ordinary SQL.
func TestReviewPortalNameCollision(t *testing.T) {
	r := newActiveRig(t, "", provenProbe)
	ctx := context.Background()
	r.exec("BEGIN")
	r.exec("DECLARE lqo_hint CURSOR FOR SELECT 1")
	if _, err := r.app.ExecOne(ctx, probe); err != nil {
		t.Errorf("hinted statement with an application cursor named lqo_hint: %v", err)
	}
	r.app.Exec(ctx, "ROLLBACK")
}

// A hinted call is a custom plan built under the hint, and PostgreSQL
// adds its cost to the prepared statement's custom-plan average either
// way. A hint that makes plans look expensive (here: no index scans)
// pushes that average up, so after five hinted calls PostgreSQL switches
// the same prepared statement to its generic plan for the calls that
// aren't hinted (holdouts, or after the hint is switched off). Without
// LQO it keeps choosing custom plans. The hint reaches other calls.
func TestReviewHintedCallsSkewPlanCacheChoice(t *testing.T) {
	const q = "SELECT count(*) FROM lqo_review_skew WHERE k = $1"
	r := newActiveRig(t, "", func(f *state.File, db string) {
		key := db + "/" + sqltext.Fingerprint(q)
		f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(q), Database: db, Status: Proven,
			Proven: "no index scans", Settings: []hints.Setting{
				{Name: "enable_indexscan", Value: "off"}, {Name: "enable_bitmapscan", Value: "off"}, {Name: "enable_indexonlyscan", Value: "off"}},
			Arms: map[string]*state.Arm{}}
	})
	ctx := context.Background()
	for _, s := range []string{
		"DROP TABLE IF EXISTS lqo_review_skew",
		"CREATE TABLE lqo_review_skew (id int, k int)",
		"INSERT INTO lqo_review_skew SELECT g, CASE WHEN g <= 20 THEN g ELSE 0 END FROM generate_series(1, 200000) g",
		"CREATE INDEX ON lqo_review_skew (k)",
		"ANALYZE lqo_review_skew",
	} {
		if _, err := r.direct.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { r.direct.Exec(context.Background(), "DROP TABLE IF EXISTS lqo_review_skew") })

	bind := func(stmt string) raw {
		var b raw
		params := []byte{0, 0, 0, 1, 0, 0, 0, 1, '1', 0, 0}
		return b.msg('B', cs(""), cs(stmt), params).msg('E', cs(""), []byte{0, 0, 0, 0}).msg('S')
	}
	run := func(stmt string, calls int) string {
		if err := r.app.SendRaw(raw(nil).msg('P', cs(stmt), cs(q), []byte{0, 0}).msg('S')); err != nil {
			t.Fatal(err)
		}
		r.replies(1)
		r.exec("BEGIN")
		for i := 0; i < calls; i++ {
			if i == 5 {
				// The shape's hint is switched off (or the call is held
				// out). With every hint switched off, LQO passes everything
				// through as it came, and this is a documented limit.
				if _, err := r.l.SwitchShape(sqltext.Fingerprint(q), true); err != nil {
					t.Fatal(err)
				}
			}
			r.app.SendRaw(bind(stmt))
			if types, _ := r.replies(1); strings.Contains(types, "E") {
				t.Fatalf("call %d: %q", i, types)
			}
		}
		r.exec("COMMIT")
		r.l.SwitchShape(sqltext.Fingerprint(q), false)
		r.l.SwitchAll(false)
		return r.simple("SELECT generic_plans || ' ' || custom_plans FROM pg_prepared_statements WHERE name = '" + stmt + "'")
	}
	// Without LQO's hint: every call gets a custom plan.
	r.l.SwitchAll(true)
	plain := run("plain", 8)
	r.l.SwitchAll(false)
	// Five hinted calls, then three that aren't hinted.
	hinted := run("hinted", 8)
	if !strings.HasPrefix(plain, "0 ") {
		t.Fatalf("control: generic/custom = %s, want no generic plans", plain)
	}
	if hinted != plain {
		t.Errorf("generic/custom plans after five hinted calls: %s, without the hint: %s", hinted, plain)
	}
}
