// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package plan

import "testing"

func scan(alias string, plan, actual, loops float64) *Node {
	return &Node{NodeType: "Seq Scan", Relation: alias + "_table", Alias: alias, PlanRows: plan, ActualRows: actual, ActualLoops: loops}
}

func join(t string, plan, actual, loops float64, kids ...*Node) *Node {
	return &Node{NodeType: t, JoinType: "Inner", PlanRows: plan, ActualRows: actual, ActualLoops: loops, Plans: kids}
}

func TestCorrections(t *testing.T) {
	// The demo trap's shape: c ⋈ o ⋈ l ⋈ p, nested loops all the way, the
	// planner expecting a handful of rows. The inner sides run once per
	// outer row, so only the outer joins are corrected.
	root := &Node{NodeType: "Aggregate", PlanRows: 1, ActualRows: 1, ActualLoops: 1, Plans: []*Node{
		join("Nested Loop", 44, 1499775, 1,
			join("Nested Loop", 44, 1499775, 1,
				join("Nested Loop", 15, 499925, 1,
					scan("c", 6, 99985, 1),
					join("Nested Loop", 5, 5, 99985, scan("x", 1, 1, 1), scan("o", 5, 5, 99985))), // inner: not counted
				scan("l", 3, 3, 499925)),
			scan("p", 1, 1, 1499775)),
	}}
	got := RowsHint(Corrections(root))
	want := "Rows(c o x *33000) Rows(c l o x *34000) Rows(c l o p x *34000)"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	// Under a Gather, rows are per process on both sides of the ratio.
	par := &Node{NodeType: "Gather", PlanRows: 100, ActualRows: 9000, ActualLoops: 1, Plans: []*Node{
		join("Hash Join", 40, 3000, 3, scan("a", 10, 10, 3), &Node{NodeType: "Hash", Plans: []*Node{scan("b", 5, 5, 3)}}),
	}}
	if got := RowsHint(Corrections(par)); got != "Rows(a b *75)" {
		t.Errorf("parallel: %s", got)
	}

	// Close enough estimates aren't touched; nor are joins over partitions
	// or with aliases LQO won't write into a comment.
	if got := Corrections(join("Hash Join", 100, 150, 1, scan("a", 1, 1, 1), scan("b", 1, 1, 1))); len(got) != 0 {
		t.Errorf("close estimate corrected: %v", got)
	}
	part := join("Hash Join", 10, 1000, 1, scan("a", 1, 1, 1), &Node{NodeType: "Append", Plans: []*Node{scan("b_1", 1, 1, 1), scan("b_2", 1, 1, 1)}})
	if got := Corrections(part); len(got) != 0 {
		t.Errorf("join over partitions corrected: %v", got)
	}
	odd := join("Hash Join", 10, 1000, 1, scan("a", 1, 1, 1), scan(`b*/x`, 1, 1, 1))
	if got := Corrections(odd); len(got) != 0 {
		t.Errorf("unsafe alias written: %v", got)
	}
	// Nothing found where the estimate was nothing: as small as it gets.
	none := Corrections(join("Hash Join", 500, 0, 1, scan("a", 1, 1, 1), scan("b", 1, 1, 1)))
	if len(none) != 1 || none[0].Factor != 0.002 {
		t.Errorf("empty join: %v", none)
	}

	m := Merge([]Correction{{Aliases: []string{"a", "b"}, Factor: 10}}, []Correction{{Aliases: []string{"a", "b"}, Factor: 99}, {Aliases: []string{"a", "b", "c"}, Factor: 3}})
	if RowsHint(m) != "Rows(a b *10) Rows(a b c *3)" {
		t.Errorf("merge: %s", RowsHint(m))
	}
}
