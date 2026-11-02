// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package stats

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/sqltext"
)

func reviewSetup(t *testing.T, stmts ...string) {
	t.Helper()
	c := connect(t)
	ctx := context.Background()
	all := append([]string{"DROP SCHEMA IF EXISTS lqo_review CASCADE", "CREATE SCHEMA lqo_review"}, stmts...)
	for _, s := range all {
		if _, err := c.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	t.Cleanup(func() { c.Exec(context.Background(), "DROP SCHEMA IF EXISTS lqo_review CASCADE") })
}

// The expression is written back without spaces between operators, so
// "now() - -col" becomes "now()--col": a line comment. A quoted column name
// that holds a newline then ends the comment, and what follows runs as SQL,
// after a COMMIT that ends the READ ONLY transaction.
func TestReviewCommentInjection(t *testing.T) {
	col := "\n;COMMIT;CREATE TABLE lqo_review.pwned();--"
	quoted := `"` + col + `"`
	reviewSetup(t,
		"CREATE TABLE lqo_review.evil (id int, "+quoted+" interval)",
		"INSERT INTO lqo_review.evil VALUES (1, '1 day')",
	)
	c := connect(t)
	ctx := context.Background()
	q := "SELECT id FROM lqo_review.evil WHERE now() - -" + quoted + " > $1"
	// The query is a valid read that the classifier accepts.
	if r := sqltext.ClassifyTemplate(q); r != "" {
		t.Fatalf("classifier refused: %s", r)
	}
	if _, err := c.Exec(ctx, strings.Replace(q, "$1", "now()", 1)); err != nil {
		t.Fatalf("query doesn't run: %v", err)
	}
	f := &Filler{Conn: c, Samples: 2, Timeout: 5 * time.Second}
	f.Fill(ctx, q)
	v, err := c.QueryValue(ctx, "SELECT coalesce(to_regclass('lqo_review.pwned')::text, '')")
	if err != nil {
		t.Fatal(err)
	}
	if v != "" {
		t.Errorf("sampling ran SQL taken from a column name outside the READ ONLY transaction: %s was created", v)
	}
}

func scopeTables(t *testing.T) {
	reviewSetup(t,
		"CREATE TABLE lqo_review.a (x int, status text)",
		"INSERT INTO lqo_review.a SELECT g, 'a_only' FROM generate_series(1, 100) g",
		"CREATE TABLE lqo_review.b (y int, status text)",
		"INSERT INTO lqo_review.b SELECT g, 'b_only' FROM generate_series(1, 100) g",
	)
}

// An unqualified column inside a subquery belongs to the subquery's own
// table when it has that column, as PostgreSQL resolves it. LQO takes the
// first table in the text that has it, the outer one.
func TestReviewSubqueryColumnScope(t *testing.T) {
	scopeTables(t)
	c := connect(t)
	f := &Filler{Conn: c, Samples: 2, Timeout: 5 * time.Second}
	q := "SELECT * FROM lqo_review.a WHERE a.x IN (SELECT y FROM lqo_review.b WHERE status = $1)"
	texts, why, err := f.Fill(context.Background(), q)
	if err != nil || len(texts) == 0 {
		t.Fatalf("%v %s", err, why)
	}
	for _, tx := range texts {
		if !strings.Contains(tx, "'b_only'") {
			t.Errorf("value from the wrong table: %s", tx)
		}
	}
}

// An alias used again inside a subquery stands for the subquery's table
// there. Aliases keeps only the first use of a name.
func TestReviewShadowedAlias(t *testing.T) {
	scopeTables(t)
	c := connect(t)
	f := &Filler{Conn: c, Samples: 2, Timeout: 5 * time.Second}
	q := "SELECT * FROM lqo_review.a t WHERE EXISTS (SELECT 1 FROM lqo_review.b t WHERE t.status = $1)"
	texts, why, err := f.Fill(context.Background(), q)
	if err != nil || len(texts) == 0 {
		t.Fatalf("%v %s", err, why)
	}
	for _, tx := range texts {
		if !strings.Contains(tx, "'b_only'") {
			t.Errorf("value from the wrong table: %s", tx)
		}
	}
}

// The sampling statements LQO runs land in pg_stat_statements as top-level
// reads. A later --stats run reads them back as the workload, although
// Summary says LQO's own statements are dropped.
func TestReviewOwnSamplingReadBack(t *testing.T) {
	c := connect(t)
	ctx := context.Background()
	ensureStats(t, c)
	scopeTables(t)
	if _, err := c.Exec(ctx, "SELECT pg_stat_statements_reset()"); err != nil {
		t.Fatal(err)
	}
	f := &Filler{Conn: c, Samples: 2, Timeout: 5 * time.Second}
	if _, why, err := f.Fill(ctx, "SELECT * FROM lqo_review.a WHERE a.status = $1"); err != nil || why != "" {
		t.Fatalf("%v %s", err, why)
	}
	entries, sum, err := Read(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Query, "lqo_review.a") {
			t.Errorf("LQO's own sampling statement read back as workload (%s): %s", sum, e.Query)
		}
	}
}

// Whatever the pieces of a query hold, a sampling statement with a
// semicolon or a comment in it never runs.
func TestQueryRefusesMoreThanOneStatement(t *testing.T) {
	c := connect(t)
	f := &Filler{Conn: c}
	for _, q := range []string{"SELECT 1; SELECT 2", "SELECT 1 -- x", "SELECT 1 /* x */"} {
		if _, err := f.query(context.Background(), q); err != ErrUnsafe {
			t.Errorf("%q: %v", q, err)
		}
	}
	if c.TxStatus() != 'I' {
		t.Errorf("transaction left open: %c", c.TxStatus())
	}
}
