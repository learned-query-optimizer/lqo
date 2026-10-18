// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"os"
	"strings"
	"testing"
)

// testdata holds real EXPLAIN (FORMAT JSON) output from PostgreSQL 16.15
// for one three-table join: the default plan, the plan with nested loops
// switched off (run with ANALYZE), and the VERBOSE plan with a query ID.

func load(t *testing.T, name string) *Explain {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	e, err := Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestParse(t *testing.T) {
	e := load(t, "hash-analyze.json")
	if !e.Analyzed || e.ExecutionMs <= 0 || e.PlanningMs <= 0 {
		t.Errorf("timings not read: %+v", e)
	}
	if e.TotalMs() != e.PlanningMs+e.ExecutionMs {
		t.Error("TotalMs")
	}
	if e.RootRows() != 1 {
		t.Errorf("root rows %v", e.RootRows())
	}
	n := load(t, "nestloop.json")
	if n.Analyzed {
		t.Error("a plain EXPLAIN isn't analyzed")
	}
	v := load(t, "verbose.json")
	if v.QueryID.String() == "" || v.QueryID.String() == "0" {
		t.Errorf("query ID not read: %q", v.QueryID)
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{"", "[]", "[{}]", "not json"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%q should fail", s)
		}
	}
}

func TestFingerprintAndSteps(t *testing.T) {
	a := load(t, "nestloop.json").Plan
	b := load(t, "hash-analyze.json").Plan
	if Fingerprint(a) == Fingerprint(b) {
		t.Error("different plans share a fingerprint")
	}
	if Fingerprint(a) != Fingerprint(load(t, "nestloop.json").Plan) {
		t.Error("fingerprint isn't stable")
	}
	steps := strings.Join(Steps(a), "; ")
	if !strings.Contains(steps, "Nested Loop") || !strings.Contains(steps, "Seq Scan on customers") {
		t.Errorf("steps: %s", steps)
	}
	ch := Change(a, b)
	if !strings.Contains(ch, "Nested Loop ×2") || !strings.Contains(ch, "Hash Join ×2") || !strings.Contains(ch, "→") {
		t.Errorf("change: %s", ch)
	}
	if Change(a, a) != "same joins and scans" {
		t.Error("a plan compared with itself should be the same")
	}
}

func TestFingerprintIgnoresCosts(t *testing.T) {
	x := &Node{NodeType: "Seq Scan", Relation: "t", PlanRows: 10}
	y := &Node{NodeType: "Seq Scan", Relation: "t", PlanRows: 9999, ActualRows: 5}
	if Fingerprint(x) != Fingerprint(y) {
		t.Error("costs and row counts changed the fingerprint")
	}
	z := &Node{NodeType: "Seq Scan", Relation: "u"}
	if Fingerprint(x) == Fingerprint(z) {
		t.Error("a different table should change the fingerprint")
	}
}
