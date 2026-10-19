// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package advisor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/demo"
	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/workload"
)

// These tests need a PostgreSQL server: set LQO_TEST_DSN to a superuser
// connection on a test database. They create the schemas lqo_test_adv and
// lqo_demo. Without LQO_TEST_DSN they skip.

const fixture = `
DROP SCHEMA IF EXISTS lqo_test_adv CASCADE;
CREATE SCHEMA lqo_test_adv;
CREATE TABLE lqo_test_adv.small AS SELECT g AS id, g % 50 AS v FROM generate_series(1, 20000) g;
ALTER TABLE lqo_test_adv.small ADD PRIMARY KEY (id);
CREATE TABLE lqo_test_adv.big AS SELECT g AS id, (g % 20000) + 1 AS small_id, g % 7 AS w FROM generate_series(1, 300000) g;
ALTER TABLE lqo_test_adv.big ADD PRIMARY KEY (id);
CREATE INDEX ON lqo_test_adv.big (small_id);
CREATE FUNCTION lqo_test_adv.writer() RETURNS int LANGUAGE plpgsql AS
  $$ BEGIN INSERT INTO lqo_test_adv.small VALUES (-1, -1); RETURN 1; END $$;
ANALYZE lqo_test_adv.small;
ANALYZE lqo_test_adv.big;
`

func dsn(t *testing.T) string {
	t.Helper()
	d := os.Getenv("LQO_TEST_DSN")
	if d == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	return d
}

func connect(t *testing.T) *pgwire.Conn {
	t.Helper()
	cfg, err := pgwire.ParseConfig(dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := pgwire.Connect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

var fixtureDone bool

func setup(t *testing.T) *pgwire.Conn {
	t.Helper()
	c := connect(t)
	if !fixtureDone {
		if _, err := c.Exec(context.Background(), fixture); err != nil {
			t.Fatal(err)
		}
		fixtureDone = true
	}
	return c
}

func newAdvisor(t *testing.T, c *pgwire.Conn, opt Options) (*Advisor, *Report) {
	t.Helper()
	opt.AllowPrimary = true
	if opt.Repeat == 0 {
		opt.Repeat = 2
	}
	rep := &Report{}
	a, err := New(context.Background(), c, opt, rep)
	if err != nil {
		t.Fatal(err)
	}
	return a, rep
}

func shapes(sql ...string) []*workload.Shape {
	var st []workload.Statement
	for _, s := range sql {
		st = append(st, workload.Statement{Text: s, LoggedMs: -1})
	}
	return workload.Group(st)
}

func checksum(t *testing.T, c *pgwire.Conn) string {
	t.Helper()
	v, err := c.QueryValue(context.Background(), "SELECT count(*) || ':' || sum(id) || ':' || sum(v) FROM lqo_test_adv.small")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func find(rep *Report, prefix string) *ShapeResult {
	for i := range rep.Shapes {
		if strings.HasPrefix(rep.Shapes[i].Example, prefix) {
			return &rep.Shapes[i]
		}
	}
	return nil
}

func TestRefusesPrimary(t *testing.T) {
	c := setup(t)
	rep := &Report{}
	_, err := New(context.Background(), c, Options{}, rep)
	if rep.Standby {
		t.Skip("the test server is a standby")
	}
	if !errors.Is(err, ErrPrimary) {
		t.Fatalf("want ErrPrimary, got %v", err)
	}
}

func TestWritesAreNeverRun(t *testing.T) {
	c := setup(t)
	before := checksum(t, c)
	a, rep := newAdvisor(t, c, Options{})
	sh := shapes(
		"INSERT INTO lqo_test_adv.small VALUES (-5, 5)",
		"UPDATE lqo_test_adv.small SET v = v + 1",
		"DELETE FROM lqo_test_adv.small",
		"WITH d AS (DELETE FROM lqo_test_adv.small RETURNING *) SELECT count(*) FROM d",
		"SELECT * INTO lqo_test_adv.copied FROM lqo_test_adv.small",
		"SELECT * FROM lqo_test_adv.small WHERE id = 1 FOR UPDATE",
		"SELECT set_config('enable_nestloop', 'off', false)",
		"TRUNCATE lqo_test_adv.small",
	)
	if err := a.Run(context.Background(), sh, rep); err != nil {
		t.Fatal(err)
	}
	for _, s := range rep.Shapes {
		if s.Verdict != Skipped {
			t.Errorf("%q: verdict %q, want skipped", s.Example, s.Verdict)
		}
	}
	if after := checksum(t, c); after != before {
		t.Errorf("data changed: %s → %s", before, after)
	}
	if v, _ := c.QueryValue(context.Background(), "SELECT to_regclass('lqo_test_adv.copied') IS NULL"); v != "t" {
		t.Error("SELECT INTO created a table")
	}
}

// Even a write that gets past the statement check can't write: every trial
// is a READ ONLY transaction, rolled back.
func TestTrialsAreReadOnly(t *testing.T) {
	c := setup(t)
	before := checksum(t, c)
	a, _ := newAdvisor(t, c, Options{})
	ctx := context.Background()
	for _, sql := range []string{
		"UPDATE lqo_test_adv.small SET v = 0",
		"SELECT lqo_test_adv.writer()",
		"DELETE FROM lqo_test_adv.small",
	} {
		for _, mode := range []string{"plan", "analyze"} {
			_, err := a.trial(ctx, sql, &a.opt.Arms[0], mode, 5*time.Second)
			if mode == "analyze" && !pgwire.IsCode(err, pgwire.CodeReadOnlySQL) {
				t.Errorf("%s (%s): want a read-only error, got %v", sql, mode, err)
			}
		}
	}
	if after := checksum(t, c); after != before {
		t.Errorf("data changed: %s → %s", before, after)
	}
	if c.TxStatus() != 'I' {
		t.Error("a transaction was left open")
	}
}

func TestSettingsNeverLeak(t *testing.T) {
	c := setup(t)
	a, rep := newAdvisor(t, c, Options{MaxTime: 300 * time.Millisecond})
	sh := shapes(
		"SELECT count(*) FROM lqo_test_adv.small s JOIN lqo_test_adv.big b ON b.small_id = s.id WHERE s.v = 3",
		"SELECT count(*) FROM lqo_test_adv.no_such_table",
		"SELECT count(*) FROM generate_series(1, 300000000)",
	)
	if err := a.Run(context.Background(), sh, rep); err != nil {
		t.Fatal(err)
	}
	for _, name := range hints.SettingNames(hints.Standard()) {
		v, err := c.QueryValue(context.Background(), "SELECT current_setting('"+name+"')")
		if err != nil {
			t.Fatal(err)
		}
		if a.snap[name] != v {
			t.Errorf("%s is %q after the run, was %q", name, v, a.snap[name])
		}
	}
	for _, name := range []string{"statement_timeout", "lock_timeout", "transaction_read_only"} {
		v, _ := c.QueryValue(context.Background(), "SHOW "+name)
		if v != map[string]string{"statement_timeout": "0", "lock_timeout": "0", "transaction_read_only": "off"}[name] {
			t.Errorf("%s is %q after the run", name, v)
		}
	}
	if s := find(rep, "SELECT count(*) FROM lqo_test_adv.no_such"); s == nil || s.Verdict != Failed || !strings.Contains(s.Reason, "does not exist") {
		t.Errorf("missing table: %+v", s)
	}
	if s := find(rep, "SELECT count(*) FROM generate_series"); s == nil || s.Verdict != TooSlow {
		t.Errorf("slow query: %+v", s)
	}
}

// A hint that makes a query slower must never be suggested, and one far
// slower is stopped at the time limit rather than left to run.
func TestSlowerHintNeverPromoted(t *testing.T) {
	c := setup(t)
	harmful := []hints.Arm{
		{Name: "no index scans at all", Settings: []hints.Setting{{Name: "enable_indexscan", Value: "off"}, {Name: "enable_indexonlyscan", Value: "off"}, {Name: "enable_bitmapscan", Value: "off"}}},
		{Name: "no nested loop", Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}}},
	}
	a, rep := newAdvisor(t, c, Options{Arms: harmful, MinTimeout: 5 * time.Millisecond, Repeat: 3})
	sh := shapes("SELECT s.v, count(*) FROM lqo_test_adv.small s JOIN lqo_test_adv.big b ON b.small_id = s.id WHERE s.id = 77 GROUP BY s.v")
	if err := a.Run(context.Background(), sh, rep); err != nil {
		t.Fatal(err)
	}
	s := rep.Shapes[0]
	if s.Verdict != Keep || s.Best != nil {
		t.Fatalf("verdict %q, best %+v", s.Verdict, s.Best)
	}
	timedOut := false
	for _, at := range s.Samples[0].Arms {
		if at.TimedOut {
			timedOut = true
		} else if !at.Same && at.Ms < s.Samples[0].BaseMs {
			t.Errorf("%q was faster than the default (%v < %v) in this setup", at.Name, at.Ms, s.Samples[0].BaseMs)
		}
	}
	if !timedOut {
		t.Error("the far slower plan should have been stopped at the time limit")
	}
}

// A query that returns a different number of rows on each run can't be
// judged, so no hint is suggested for it.
func TestDifferentRowsRuleOut(t *testing.T) {
	c := setup(t)
	a, rep := newAdvisor(t, c, Options{Repeat: 2})
	// The rows this returns change on every run, so no plan can be
	// compared with the default one.
	sh := shapes("SELECT s.id FROM lqo_test_adv.small s JOIN lqo_test_adv.big b ON b.small_id = s.id WHERE random() < 0.5")
	if err := a.Run(context.Background(), sh, rep); err != nil {
		t.Fatal(err)
	}
	s := rep.Shapes[0]
	if s.Verdict == Faster {
		t.Errorf("a hint was suggested for a query whose rows change: %+v", s.Best)
	}
	if !strings.Contains(s.Reason, "different number of rows") {
		t.Errorf("reason %q doesn't say the rows changed", s.Reason)
	}
}

func TestQuotedLockIsSkipped(t *testing.T) {
	c := setup(t)
	a, rep := newAdvisor(t, c, Options{})
	sh := shapes(`SELECT "pg_advisory_lock"(42)`)
	if err := a.Run(context.Background(), sh, rep); err != nil {
		t.Fatal(err)
	}
	if rep.Shapes[0].Verdict != Skipped {
		t.Errorf("verdict %s, want skipped", rep.Shapes[0].Verdict)
	}
}

func TestQueryIDAndSessionSettings(t *testing.T) {
	c := setup(t)
	a, rep := newAdvisor(t, c, Options{Session: []hints.Setting{{Name: "search_path", Value: "lqo_test_adv"}}})
	sh := shapes("SELECT count(*) FROM small WHERE v = 1", "SELECT count(*) FROM small WHERE v = 2")
	if err := a.Run(context.Background(), sh, rep); err != nil {
		t.Fatal(err)
	}
	s := rep.Shapes[0]
	if s.Verdict == Failed {
		t.Fatalf("search_path not applied: %s", s.Reason)
	}
	if len(s.Samples) != 2 || s.Calls != 2 {
		t.Errorf("samples %d, calls %d", len(s.Samples), s.Calls)
	}
	if rep.ServerNum >= 140000 && s.QueryID == "" {
		t.Error("no query ID on PostgreSQL 14+")
	}
}

func TestInterruptStopsQuickly(t *testing.T) {
	c := setup(t)
	a, rep := newAdvisor(t, c, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	sh := shapes("SELECT count(*) FROM generate_series(1, 200000000)", "SELECT 1")
	err := a.Run(ctx, sh, rep)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Interrupted {
		t.Error("the report should say it was interrupted")
	}
	if d := time.Since(start); d > 8*time.Second {
		t.Errorf("stopping took %s", d)
	}
	if !c.Broken() {
		if v, err := c.QueryValue(context.Background(), "SELECT current_setting('statement_timeout')"); err != nil || v != "0" {
			t.Errorf("after an interrupt: %q %v", v, err)
		}
	}
}

// TestSyntheticBenchmark builds the demo database and runs its workload.
// LQO must find the nested-loop trap, and must leave alone the shapes it
// shouldn't touch. Set LQO_TEST_DEMO=skip to leave it out.
func TestSyntheticBenchmark(t *testing.T) {
	if os.Getenv("LQO_TEST_DEMO") == "skip" || testing.Short() {
		t.Skip("demo benchmark skipped")
	}
	c := connect(t)
	ctx := context.Background()
	start := time.Now()
	if err := demo.Setup(ctx, c, 1, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("demo built in %s", time.Since(start).Round(time.Millisecond))
	a, rep := newAdvisor(t, c, Options{Repeat: 3})
	st := workload.ReadSQL("demo.sql", demo.Workload)
	if err := a.Run(ctx, workload.Group(st), rep); err != nil {
		t.Fatal(err)
	}
	trap := find(rep, "SELECT sum(l.qty * p.price)")
	if trap == nil || trap.Verdict != Faster {
		t.Fatalf("the nested-loop trap wasn't found: %+v", trap)
	}
	offNestloop := false
	for _, s := range trap.Best.Settings {
		if s.Name == "enable_nestloop" && s.Value == "off" {
			offNestloop = true
		}
	}
	if !offNestloop || trap.Best.Gain < 0.2 {
		t.Errorf("trap: hint %q, gain %.2f", trap.Best.Name, trap.Best.Gain)
	}
	t.Logf("trap: %.0f ms → %.0f ms per pass of two texts with %q (−%.0f%%)", trap.BaselineMs, trap.Best.Ms, trap.Best.Name, trap.Best.Gain*100)
	if s := find(rep, "SELECT id FROM lqo_demo.customers WHERE attrs->>'channel'"); s == nil || s.Verdict == Faster {
		t.Errorf("a hint was suggested for the shape where it hurts one value: %+v", s)
	}
	if s := find(rep, "SELECT c.id, count(*)"); s == nil || s.Verdict == Faster {
		t.Errorf("a hint was suggested for the key lookup: %+v", s)
	}
	if s := find(rep, "UPDATE"); s == nil || s.Verdict != Skipped {
		t.Errorf("the UPDATE wasn't skipped: %+v", s)
	}
	for _, s := range rep.Shapes {
		t.Logf("%s %-9s %s", s.ID[:8], s.Verdict, s.Reason)
	}
}
