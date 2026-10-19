// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package advisor

import (
	"context"
	"strings"
	"testing"
)

// With pg_hint_plan, LQO builds a hint from what the default plan's run
// measured: the joins whose estimates were far off, corrected. It's tried
// like any other hint, on every text, by the same rule, and leaves nothing
// behind.
func TestRowCountHints(t *testing.T) {
	c := connect(t)
	ctx := context.Background()
	if _, err := c.Exec(ctx, `DROP SCHEMA IF EXISTS lqo_test_rows CASCADE;
CREATE SCHEMA lqo_test_rows;
CREATE TABLE lqo_test_rows.a AS SELECT g AS id, g % 50 AS x, g % 50 AS y FROM generate_series(1, 50000) g;
CREATE TABLE lqo_test_rows.b AS SELECT g AS id, (g % 50000) + 1 AS a_id, g % 7 AS v FROM generate_series(1, 250000) g;
CREATE TABLE lqo_test_rows.c AS SELECT g AS id, g % 7 AS v, 'v' || g AS label FROM generate_series(0, 6) g;
CREATE INDEX ON lqo_test_rows.b (a_id);
ANALYZE lqo_test_rows.a; ANALYZE lqo_test_rows.b; ANALYZE lqo_test_rows.c;`); err != nil {
		t.Fatal(err)
	}
	defer c.Exec(ctx, "DROP SCHEMA lqo_test_rows CASCADE")
	rep := &Report{}
	a, err := New(ctx, c, Options{AllowPrimary: true, Repeat: 2, JoinHints: "on"}, rep)
	if err != nil {
		if strings.Contains(err.Error(), "pg_hint_plan") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if !strings.HasPrefix(rep.JoinHints, "pg_hint_plan") {
		t.Fatalf("join hints: %q", rep.JoinHints)
	}
	q := func(k string) string {
		return "SELECT count(*), max(c.label) FROM lqo_test_rows.a a JOIN lqo_test_rows.b b ON b.a_id = a.id JOIN lqo_test_rows.c c ON c.v = b.v WHERE a.x = " + k + " AND a.y = " + k
	}
	if err := a.Run(ctx, shapes(q("3"), q("4")), rep); err != nil {
		t.Fatal(err)
	}
	r := rep.Shapes[0]
	if len(r.Samples) != 2 {
		t.Fatalf("samples: %+v", r)
	}
	for _, s := range r.Samples {
		found := false
		for _, at := range s.Arms {
			if at.Name != "row counts" {
				continue
			}
			found = true
			if at.Error != "" || (!at.Same && at.Rows != s.BaseRows) {
				t.Errorf("row counts on %q: %+v", s.Text, at)
			}
		}
		if !found {
			t.Errorf("no row-count hint tried on %q: %+v", s.Text, s.Arms)
		}
	}
	t.Logf("verdict %s: %s; best %+v", r.Verdict, r.Reason, r.Best)
	if r.Best != nil && r.Best.Comment != "" && !strings.HasPrefix(r.Best.Comment, "Rows(") {
		t.Errorf("comment %q", r.Best.Comment)
	}
	if a.extra != nil {
		t.Error("the shape's row-count hints outlived it")
	}
	// Nothing left behind, pg_hint_plan's own setting included.
	if err := a.checkNoLeak(ctx); err != nil {
		t.Fatal(err)
	}

	// Off means off, even with the extension there.
	rep2 := &Report{}
	b, err := New(ctx, c, Options{AllowPrimary: true, Repeat: 1, JoinHints: "off"}, rep2)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Run(ctx, shapes(q("5")), rep2); err != nil {
		t.Fatal(err)
	}
	for _, at := range rep2.Shapes[0].Samples[0].Arms {
		if strings.HasPrefix(at.Name, "row counts") {
			t.Errorf("--join-hints off tried %q", at.Name)
		}
	}
}
