// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package sqltext

import "testing"

// The calculation rule runs after the BETWEEN and LIKE branches of
// paramUse, so a parameter that's an operand of arithmetic there is still
// given the column's values.
func TestReviewArithmeticInBetweenAndLike(t *testing.T) {
	for q, n := range map[string]int{
		"SELECT * FROM t WHERE t.b BETWEEN $1 * t.c AND $2":  1,
		"SELECT * FROM t WHERE t.b BETWEEN t.c AND $1 + t.d": 1,
		"SELECT * FROM t WHERE t.name LIKE $1 || t.suffix":   1,
	} {
		if u := ParamUses(q)[n]; u.Reason == "" {
			t.Errorf("%s: $%d taken as %s %s, but it's an operand of a calculation", q, n, u.Use, u.Expr)
		}
	}
}
