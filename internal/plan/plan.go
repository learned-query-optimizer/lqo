// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package plan reads the JSON that EXPLAIN (FORMAT JSON) returns.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Node is one step of a query plan.
type Node struct {
	NodeType      string  `json:"Node Type"`
	JoinType      string  `json:"Join Type"`
	Strategy      string  `json:"Strategy"`
	PartialMode   string  `json:"Partial Mode"`
	Relation      string  `json:"Relation Name"`
	Schema        string  `json:"Schema"`
	Alias         string  `json:"Alias"`
	Index         string  `json:"Index Name"`
	ScanDirection string  `json:"Scan Direction"`
	ParallelAware bool    `json:"Parallel Aware"`
	Parent        string  `json:"Parent Relationship"`
	SubplanName   string  `json:"Subplan Name"`
	CTEName       string  `json:"CTE Name"`
	FunctionName  string  `json:"Function Name"`
	PlanRows      float64 `json:"Plan Rows"`
	ActualRows    float64 `json:"Actual Rows"`
	ActualLoops   float64 `json:"Actual Loops"`
	Plans         []*Node `json:"Plans"`
}

// Explain is one EXPLAIN result.
type Explain struct {
	Plan        *Node       `json:"Plan"`
	PlanningMs  float64     `json:"Planning Time"`
	ExecutionMs float64     `json:"Execution Time"`
	QueryID     json.Number `json:"Query Identifier"`
	Analyzed    bool        `json:"-"`
}

// Parse reads EXPLAIN (FORMAT JSON) output.
func Parse(text string) (*Explain, error) {
	var arr []json.RawMessage
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&arr); err != nil {
		return nil, fmt.Errorf("reading EXPLAIN output: %w", err)
	}
	if len(arr) == 0 {
		return nil, errors.New("EXPLAIN returned nothing")
	}
	var e Explain
	if err := json.Unmarshal(arr[0], &e); err != nil {
		return nil, fmt.Errorf("reading EXPLAIN output: %w", err)
	}
	if e.Plan == nil {
		return nil, errors.New("EXPLAIN output has no plan")
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(arr[0], &raw) == nil {
		_, e.Analyzed = raw["Execution Time"]
	}
	return &e, nil
}

// TotalMs is planning time plus execution time, as EXPLAIN ANALYZE reports.
func (e *Explain) TotalMs() float64 { return e.PlanningMs + e.ExecutionMs }

// RootRows is how many rows the whole query returned.
func (e *Explain) RootRows() float64 {
	loops := e.Plan.ActualLoops
	if loops == 0 {
		loops = 1
	}
	return e.Plan.ActualRows * loops
}

// Fingerprint identifies the plan's structure: node types, join types,
// tables and indexes, but not costs or row counts. Two arms that produce
// the same fingerprint run the same plan.
func Fingerprint(n *Node) string {
	var b strings.Builder
	writeShape(&b, n)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

func writeShape(b *strings.Builder, n *Node) {
	if n == nil {
		return
	}
	b.WriteString(n.NodeType)
	for _, s := range []string{n.JoinType, n.Strategy, n.PartialMode, n.Relation, n.Alias, n.Index, n.ScanDirection, n.Parent, n.SubplanName, n.CTEName, n.FunctionName} {
		b.WriteByte('|')
		b.WriteString(s)
	}
	if n.ParallelAware {
		b.WriteString("|parallel")
	}
	b.WriteByte('(')
	for _, c := range n.Plans {
		writeShape(b, c)
		b.WriteByte(',')
	}
	b.WriteByte(')')
}

// Steps lists the plan's joins and scans in order, such as "Hash Join" or
// "Seq Scan on orders". Other nodes (sorts, aggregates, gathers) are left
// out because the settings LQO tries are about joins and scans.
func Steps(n *Node) []string {
	var out []string
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if s := step(n); s != "" {
			out = append(out, s)
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(n)
	return out
}

func step(n *Node) string {
	t := strings.TrimPrefix(n.NodeType, "Parallel ")
	switch {
	case t == "Nested Loop" || t == "Hash Join" || t == "Merge Join":
		if n.JoinType != "" && n.JoinType != "Inner" {
			return fmt.Sprintf("%s (%s)", t, strings.ToLower(n.JoinType))
		}
		return t
	case strings.HasSuffix(t, "Scan") && n.Relation != "":
		return fmt.Sprintf("%s on %s", t, n.Relation)
	case t == "Memoize":
		return t
	}
	return ""
}

// Change describes how plan b differs from plan a in its joins and scans,
// such as "Nested Loop ×2 → Hash Join ×2". It is "same joins and scans"
// when only other parts of the plan differ.
func Change(a, b *Node) string {
	count := func(n *Node) map[string]int {
		m := map[string]int{}
		for _, s := range Steps(n) {
			m[s]++
		}
		return m
	}
	ca, cb := count(a), count(b)
	// List the differences in plan order, top of the plan first, so joins
	// come before the scans under them.
	diff := func(n *Node, mine, other map[string]int) []string {
		var out []string
		seen := map[string]bool{}
		for _, s := range Steps(n) {
			if seen[s] {
				continue
			}
			seen[s] = true
			if d := mine[s] - other[s]; d > 0 {
				out = append(out, times(s, d))
			}
		}
		return out
	}
	gone, came := diff(a, ca, cb), diff(b, cb, ca)
	if len(gone) == 0 && len(came) == 0 {
		return "same joins and scans"
	}
	left, right := strings.Join(gone, ", "), strings.Join(came, ", ")
	if left == "" {
		left = "nothing"
	}
	if right == "" {
		right = "nothing"
	}
	return left + " → " + right
}

func times(s string, n int) string {
	if n == 1 {
		return s
	}
	return fmt.Sprintf("%s ×%d", s, n)
}
