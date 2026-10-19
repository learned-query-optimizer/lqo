// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package learn

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/proxy"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/state"
	"github.com/learned-query-optimizer/lqo/internal/watch"
)

// The probe is a query whose answer shows whether a hint was applied: it
// returns the setting the hint changes. Its shape is given a proven hint in
// the state file, and the test reads the answer through the proxy.
const probe = "SELECT current_setting('enable_nestloop') AS v"

type activeRig struct {
	t      *testing.T
	l      *Learner
	col    *watch.Collector
	app    *pgwire.Conn
	appCfg *pgwire.Config
	direct *pgwire.Conn
	db     string
}

func newActiveRig(t *testing.T, statePath string, preload func(f *state.File, db string)) *activeRig {
	t.Helper()
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg, _ := pgwire.ParseConfig(dsn)
	direct, err := pgwire.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { direct.Close() })
	db := cfg.Database

	if statePath == "" {
		statePath = filepath.Join(t.TempDir(), "state.json")
	}
	f := state.New()
	preload(f, db)
	if err := f.Save(statePath); err != nil {
		t.Fatal(err)
	}

	col := watch.NewCollector()
	t.Cleanup(col.Close)
	up := *cfg
	up.SSLMode = "disable"
	ln, err := proxy.Listen("127.0.0.1:0", 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := proxy.New(proxy.Config{Upstream: &up, Watch: col})
	go srv.Serve(ctx, ln)
	t.Cleanup(func() { srv.Shutdown(time.Second) })

	l, _, err := New(col, Options{Replica: cfg, AllowPrimary: true, StatePath: statePath, Active: true, Holdout: 0, MinMs: 1e9})
	if err != nil {
		t.Fatal(err)
	}
	l.Publish()

	u, _ := url.Parse(dsn)
	u.Host = ln.Addr().String()
	appCfg, _ := pgwire.ParseConfig(u.String())
	app, err := pgwire.Connect(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	return &activeRig{t: t, l: l, col: col, app: app, appCfg: appCfg, direct: direct, db: db}
}

func provenProbe(f *state.File, db string) {
	key := db + "/" + sqltext.Fingerprint(probe)
	f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(probe), Database: db, Status: Proven,
		Proven: "no nested loop", Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}}, Arms: map[string]*state.Arm{}}
}

// simple runs q with the simple protocol through the proxy.
func (r *activeRig) simple(q string) string {
	r.t.Helper()
	v, err := r.app.QueryValue(context.Background(), q)
	if err != nil {
		r.t.Fatalf("%s: %v", q, err)
	}
	return v
}

// extended runs q with the extended protocol through the proxy.
func (r *activeRig) extended(q string) string {
	r.t.Helper()
	res, err := r.app.ExecOne(context.Background(), q)
	if err != nil {
		r.t.Fatalf("%s: %v", q, err)
	}
	return *res[0].Rows[0][0]
}

func TestActiveHintIsAppliedAndNeverLeaks(t *testing.T) {
	r := newActiveRig(t, "", provenProbe)
	for _, run := range []func(string) string{r.simple, r.extended} {
		if v := run(probe); v != "off" {
			t.Fatalf("the hint wasn't applied: enable_nestloop = %s", v)
		}
		// The very next statement in the same session sees nothing of it.
		if v := run("SHOW enable_nestloop"); v != "on" {
			t.Errorf("the hint leaked: enable_nestloop = %s after the hinted query", v)
		}
		if v := run("SHOW plan_cache_mode"); v != "auto" {
			t.Errorf("plan_cache_mode leaked: %s", v)
		}
	}
	// Inside the application's own transaction, nothing is applied: a
	// SET LOCAL there would last to the end of that transaction.
	ctx := context.Background()
	if _, err := r.app.Exec(ctx, "BEGIN"); err != nil {
		t.Fatal(err)
	}
	if v := r.simple(probe); v != "on" {
		t.Error("a hint was applied inside the application's transaction")
	}
	if v := r.extended(probe); v != "on" {
		t.Error("a hint was applied inside the application's transaction (extended)")
	}
	r.app.Exec(ctx, "COMMIT")
	// Errors still point at the right place in the application's text.
	_, err := r.app.Exec(ctx, "SELECT current_setting('enable_nestloop') AS v WHERE no_such_column = 1")
	var pe *pgwire.PgError
	if !errors.As(err, &pe) || pe.Code != "42703" {
		t.Fatalf("want an undefined-column error, got %v", err)
	}
	// And the session is fine afterwards.
	if v := r.simple(probe); v != "off" {
		t.Errorf("after an error: %s", v)
	}
}

func TestErrorPositionThroughTheProxy(t *testing.T) {
	r := newActiveRig(t, "", func(f *state.File, db string) {
		q := "SELECT current_setting('enable_nestloop') AS v WHERE nope = 1"
		key := db + "/" + sqltext.Fingerprint(q)
		f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(q), Database: db, Status: Proven,
			Proven: "no nested loop", Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}}, Arms: map[string]*state.Arm{}}
	})
	q := "SELECT current_setting('enable_nestloop') AS v WHERE nope = 1"
	_, perr := r.direct.Exec(context.Background(), q)
	_, aerr := r.app.Exec(context.Background(), q)
	var dp, ap *pgwire.PgError
	if !errors.As(perr, &dp) || !errors.As(aerr, &ap) {
		t.Fatalf("%v / %v", perr, aerr)
	}
	if dp.Position != ap.Position || dp.Position == "" {
		t.Errorf("error position %q through the proxy, %q straight", ap.Position, dp.Position)
	}
}

func TestSwitchesActAtOnce(t *testing.T) {
	r := newActiveRig(t, "", provenProbe)
	if v := r.simple(probe); v != "off" {
		t.Fatal("not applied to start with")
	}
	if err := r.l.SwitchAll(true); err != nil {
		t.Fatal(err)
	}
	if v := r.simple(probe); v != "on" {
		t.Error("applied with every hint switched off")
	}
	if err := r.l.SwitchAll(false); err != nil {
		t.Fatal(err)
	}
	if v := r.extended(probe); v != "off" {
		t.Error("not applied after switching back on")
	}
	if _, err := r.l.SwitchShape(sqltext.Fingerprint(probe)[:6], true); err != nil {
		t.Fatal(err)
	}
	if v := r.simple(probe); v != "on" {
		t.Error("applied with the shape switched off")
	}
	if _, err := r.l.SwitchShape("ffffffff", true); !errors.Is(err, ErrNoShape) {
		t.Errorf("unknown shape: %v", err)
	}
}

func TestFailSafe(t *testing.T) {
	// A state file that can't be written: LQO stops applying hints.
	dir := t.TempDir()
	statePath := filepath.Join(dir, "sub", "state.json")
	os.MkdirAll(filepath.Dir(statePath), 0o755)
	r := newActiveRig(t, statePath, provenProbe)
	if v := r.simple(probe); v != "off" {
		t.Fatal("not applied to start with")
	}
	os.RemoveAll(filepath.Dir(statePath))
	if err := r.l.SwitchAll(false); err == nil {
		t.Fatal("saving into a missing folder worked")
	}
	if v := r.simple(probe); v != "on" {
		t.Error("hints still applied while the state file can't be written")
	}
	if st := r.l.Stats(); !strings.Contains(st.Paused, "state file") {
		t.Errorf("paused: %q", st.Paused)
	}
	// It comes back once the file can be written again.
	os.MkdirAll(filepath.Dir(statePath), 0o755)
	if err := r.l.SwitchAll(false); err != nil {
		t.Fatal(err)
	}
	if v := r.simple(probe); v != "off" {
		t.Error("not applied again after the state file came back")
	}

	// The replica out of reach: the same.
	r.l.fail(errors.New("replica gone"))
	if v := r.simple(probe); v != "on" {
		t.Error("hints still applied while the replica can't be reached")
	}
}

func TestGuardTakesBackASlowerHint(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	ctx := context.Background()
	cfg, _ := pgwire.ParseConfig(dsn)
	c, err := pgwire.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Exec(ctx, `DROP SCHEMA IF EXISTS lqo_test_active CASCADE;
CREATE SCHEMA lqo_test_active;
CREATE TABLE lqo_test_active.t AS SELECT g AS id, md5(g::text) AS v FROM generate_series(1, 300000) g;
ALTER TABLE lqo_test_active.t ADD PRIMARY KEY (id);
ANALYZE lqo_test_active.t;`); err != nil {
		t.Fatal(err)
	}
	defer c.Exec(ctx, "DROP SCHEMA lqo_test_active CASCADE")
	// A key lookup "proven" to be better without index scans: a lie the
	// guard must catch, since live it turns 0.05 ms into a sequential scan.
	q := "SELECT v FROM lqo_test_active.t WHERE id = 4242"
	r := newActiveRig(t, "", func(f *state.File, db string) {
		key := db + "/" + sqltext.Fingerprint(q)
		f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(q), Database: db, Status: Proven, Proven: "no index scan",
			Settings: []hints.Setting{{Name: "enable_indexscan", Value: "off"}, {Name: "enable_bitmapscan", Value: "off"}, {Name: "enable_indexonlyscan", Value: "off"}},
			Arms:     map[string]*state.Arm{"no index scan": {N: 3}}}
	})
	// Some calls before, to compare against, made with the hint off.
	r.l.SwitchAll(true)
	for i := 0; i < 30; i++ {
		r.extended(q)
	}
	r.l.SwitchAll(false)
	for i := 0; i < 30; i++ {
		r.extended(q)
	}
	r.col.Flush()
	r.l.Guard()
	sh := r.l.Shapes()
	var e *state.Learned
	for i := range sh {
		if sh[i].ID == sqltext.Fingerprint(q) {
			e = &sh[i]
		}
	}
	if e == nil || e.Status == Proven || e.Reverted == "" {
		t.Fatalf("the guard didn't take the hint back: %+v", e)
	}
	t.Logf("guard: %s", e.Reverted)
	if !strings.Contains(e.Arms["no index scan"].Out, "slower live") {
		t.Errorf("arm: %+v", e.Arms["no index scan"])
	}
	if r.l.Stats().Applied != 0 {
		t.Error("the hint is still applied")
	}
}

// Hints applied or not, the application gets the same answers.
func TestSameAnswersWithHints(t *testing.T) {
	queries := []string{
		"SELECT count(*), sum(c.oid::int) FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid WHERE a.attnum > 2",
		"SELECT relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'pg_catalog' ORDER BY relname LIMIT 5",
	}
	r := newActiveRig(t, "", func(f *state.File, db string) {
		for _, q := range queries {
			key := db + "/" + sqltext.Fingerprint(q)
			f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(q), Database: db, Status: Proven, Proven: "hash join only",
				Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}, {Name: "enable_mergejoin", Value: "off"}}, Arms: map[string]*state.Arm{}}
		}
	})
	ctx := context.Background()
	for _, q := range queries {
		for _, ext := range []bool{false, true} {
			var want, got []pgwire.Result
			var err error
			if ext {
				want, err = r.direct.ExecOne(ctx, q)
				if err == nil {
					got, err = r.app.ExecOne(ctx, q)
				}
			} else {
				want, err = r.direct.Exec(ctx, q)
				if err == nil {
					got, err = r.app.Exec(ctx, q)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(want) != len(got) || len(want[0].Rows) != len(got[0].Rows) {
				t.Fatalf("%s: different results", q)
			}
			for i := range want[0].Rows {
				for j := range want[0].Rows[i] {
					if *want[0].Rows[i][j] != *got[0].Rows[i][j] {
						t.Errorf("%s: row %d differs", q, i)
					}
				}
			}
		}
	}
	r.col.Flush()
	hinted := int64(0)
	for _, s := range r.col.Snapshot() {
		hinted += s.HintedCalls
	}
	if hinted != 4 {
		t.Errorf("%d calls hinted, want 4", hinted)
	}
}

// planProbe shows the plan it gets: a join a pg_hint_plan comment can turn
// into a merge join.
const planProbe = "EXPLAIN (COSTS OFF) SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace"

// A proven row-count hint goes in as a pg_hint_plan comment: in front of a
// plain query, and into the text of a one-shot prepared statement. A session
// without pg_hint_plan gets nothing, and its calls aren't counted as hinted.
func TestJoinHintCommentIsApplied(t *testing.T) {
	key := ""
	r := newActiveRig(t, "", func(f *state.File, db string) {
		key = db + "/" + sqltext.Fingerprint(planProbe)
		f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(planProbe), Database: db, Status: Proven,
			Proven: hints.RowCounts, Comment: "MergeJoin(c n)", Arms: map[string]*state.Arm{}}
	})
	ctx := context.Background()
	if v, err := r.direct.QueryValue(ctx, "SELECT count(*) FROM pg_available_extensions WHERE name = 'pg_hint_plan'"); err != nil || v == "0" {
		t.Skip("pg_hint_plan isn't installed")
	}
	cfg := *r.appCfg
	cfg.Options = "-c session_preload_libraries=pg_hint_plan"
	hp, err := pgwire.Connect(ctx, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer hp.Close()
	top := func(c *pgwire.Conn, extended bool) string {
		t.Helper()
		var res []pgwire.Result
		var err error
		if extended {
			res, err = c.ExecOne(ctx, planProbe)
		} else {
			res, err = c.Exec(ctx, planProbe)
		}
		if err != nil {
			t.Fatal(err)
		}
		return *res[0].Rows[0][0]
	}
	for _, ext := range []bool{false, true} {
		if v := top(hp, ext); !strings.Contains(v, "Merge Join") {
			t.Errorf("extended=%v: the comment wasn't applied: %q", ext, v)
		}
	}
	// Without pg_hint_plan in the session, the comment does nothing, and
	// LQO notices and stops putting it in. Switched off for the session,
	// whether or not the server preloads it.
	off := *r.appCfg
	off.Options = "-c pg_hint_plan.enable_hint=off"
	noHP, err := pgwire.Connect(ctx, &off)
	if err != nil {
		t.Fatal(err)
	}
	defer noHP.Close()
	for i := 0; i < 2; i++ {
		if v := top(noHP, false); strings.Contains(v, "Merge Join") {
			t.Errorf("merge join without pg_hint_plan: %q", v)
		}
	}
	r.col.Flush()
	sh, _ := r.col.Get(key)
	if sh.HintedCalls != 2 {
		t.Errorf("hinted calls %d, want the 2 with pg_hint_plan", sh.HintedCalls)
	}
	if n := r.col.Stats().NoHintPlan; n != 1 {
		t.Errorf("calls without pg_hint_plan noticed: %d, want 1 (then it stops trying)", n)
	}
	// An error in a hinted statement points into the client's own text.
	bad := planProbe + " WHERE nonexistent_column = 1"
	key2 := r.db + "/" + sqltext.Fingerprint(bad)
	r.l.mu.Lock()
	r.l.file.Serve[key2] = &state.Learned{Key: key2, ID: sqltext.Fingerprint(bad), Database: r.db, Status: Proven, Proven: hints.RowCounts, Comment: "MergeJoin(c n)", Arms: map[string]*state.Arm{}}
	r.l.publishLocked()
	r.l.mu.Unlock()
	for _, ext := range []bool{false, true} {
		var err error
		if ext {
			_, err = hp.ExecOne(ctx, bad)
		} else {
			_, err = hp.Exec(ctx, bad)
		}
		var pe *pgwire.PgError
		if !errors.As(err, &pe) || pe.Position != strconv.Itoa(strings.Index(bad, "nonexistent_column")+1) {
			t.Errorf("extended=%v: error position %+v for %q", ext, pe, bad)
		}
	}
}
