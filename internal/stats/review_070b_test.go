// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package stats

import (
	"context"
	"strings"
	"testing"
	"time"
)

func fillOne(t *testing.T, q string) []string {
	t.Helper()
	c := connect(t)
	ctx := context.Background()
	if _, err := c.Exec(ctx, "SET search_path = lqo_review, public"); err != nil {
		t.Fatal(err)
	}
	f := &Filler{Conn: c, Samples: 2, Timeout: 5 * time.Second}
	texts, why, err := f.Fill(ctx, q)
	if err != nil || len(texts) == 0 {
		t.Fatalf("%s: %v %s", q, err, why)
	}
	return texts
}

// Each branch of UNION, INTERSECT or EXCEPT is its own query level: the
// second branch's status is b's, not a's. scopes() opens a level only at a
// parenthesis, so both branches share level 0 and a comes first.
func TestReviewUnionBranchScope(t *testing.T) {
	scopeTables(t)
	for _, q := range []string{
		"SELECT x FROM lqo_review.a WHERE status = $1 UNION SELECT y FROM lqo_review.b WHERE status = $2",
		"SELECT x FROM lqo_review.a t WHERE t.status = $1 UNION ALL SELECT y FROM lqo_review.b t WHERE t.status = $2",
	} {
		for _, tx := range fillOne(t, q) {
			second := tx[strings.Index(tx, "UNION"):]
			if !strings.Contains(second, "'b_only'") {
				t.Errorf("second branch's value from the first branch's table: %s", tx)
			}
		}
	}
}

// A CTE's name is a FROM item that isn't a table. It's taken for a table of
// that name, so when one exists on the search_path its rows are sampled
// instead of the CTE's: here b is the CTE over a, but table b is read.
func TestReviewCTENameIsNotATable(t *testing.T) {
	scopeTables(t)
	q := "WITH b AS (SELECT * FROM lqo_review.a) SELECT * FROM b WHERE b.status = $1"
	c := connect(t)
	ctx := context.Background()
	if _, err := c.Exec(ctx, "SET search_path = lqo_review, public"); err != nil {
		t.Fatal(err)
	}
	f := &Filler{Conn: c, Samples: 2, Timeout: 5 * time.Second}
	texts, why, err := f.Fill(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range texts {
		if strings.Contains(tx, "'b_only'") {
			t.Errorf("value from table b, but b here is the CTE over a (why %q): %s", why, tx)
		}
	}
}
