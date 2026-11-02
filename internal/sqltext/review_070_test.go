// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package sqltext

import "testing"

// A parameter inside arithmetic isn't the value of the column on the other
// side: in t.a + $1 = t.b the value is t.b - t.a, not t.b. The design says
// arithmetic is skipped with its reason; ParamUses takes t.b's values.
func TestReviewArithmeticIsNotACompare(t *testing.T) {
	for _, q := range []string{
		"SELECT * FROM t WHERE t.a + $1 = t.b",
		"SELECT * FROM t WHERE t.a = $1 + t.b",
		"SELECT * FROM t WHERE t.a > $1 * t.b",
		"SELECT * FROM t WHERE t.a = $1 || t.b",
	} {
		u := ParamUses(q)[1]
		if u.Reason == "" {
			t.Errorf("%s: $1 taken as %s %s, but it's an operand of arithmetic", q, u.Use, u.Expr)
		}
	}
}

// The expression written back must lex as it did in the query: "- -" joined
// without a space is "--", a comment, so x - -y = $1 can't be sampled (and
// see stats.TestReviewCommentInjection for what a newline after it does).
func TestReviewExpressionRoundTrip(t *testing.T) {
	q := "SELECT * FROM t WHERE t.a - -t.b = $1"
	u := ParamUses(q)[1]
	if u.Reason != "" {
		return
	}
	var ops int
	for _, tok := range Lex(u.Expr) {
		if tok.Kind == Comment {
			t.Fatalf("expression %q lexes with a comment", u.Expr)
		}
		if tok.Kind == Op {
			ops++
		}
	}
}

// A schema-qualified column (s.t.a) names one table, but it's read as two
// qualifiers and the shape is skipped as "more than one table".
func TestReviewSchemaQualifiedColumn(t *testing.T) {
	u := ParamUses("SELECT * FROM app.orders WHERE app.orders.total > $1")[1]
	if u.Reason != "" {
		t.Errorf("skipped: %s", u.Reason)
	}
}
