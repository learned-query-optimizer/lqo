// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Correction is how far the planner's estimate for one join was from what
// EXPLAIN ANALYZE measured: the actual rows are Factor times the estimate.
// Aliases name the tables the join covers, as the query names them.
type Correction struct {
	Aliases []string
	Factor  float64
}

// Key identifies the join a correction is for.
func (c Correction) Key() string { return strings.Join(c.Aliases, " ") }

// RowsFactorMin is how far off an estimate must be, either way, before LQO
// corrects it.
const RowsFactorMin = 2.0

// safeAlias is what LQO will write inside a hint comment: a plain lower-case
// identifier, so nothing in it can end the comment or need quoting.
var safeAlias = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Corrections reads an analyzed plan and returns, for every join whose
// estimate was off by more than RowsFactorMin, the factor that would have
// made it right, sorted by key.
//
// A join counts only where its numbers can be trusted as the join's own
// size: not inside the inner side of a nested loop or a subplan, which run
// once per outer row, and not over partitions or anything LQO can't name
// with a plain alias. Under a Gather, every process reports its share, and
// the estimate is per process too, so the ratio holds.
func Corrections(root *Node) []Correction {
	var out []Correction
	seen := map[string]bool{}
	var walk func(n *Node, parallel bool) ([]string, bool)
	// walk returns the aliases under n and whether they can all be named.
	walk = func(n *Node, parallel bool) ([]string, bool) {
		if n == nil {
			return nil, false
		}
		t := n.NodeType
		if t == "Gather" || t == "Gather Merge" {
			parallel = true
		}
		var aliases []string
		ok := true
		switch {
		case n.Alias != "" && (strings.HasSuffix(t, "Scan") || t == "Subquery Scan" || t == "CTE Scan"):
			if !safeAlias.MatchString(n.Alias) {
				ok = false
			}
			aliases = append(aliases, n.Alias)
			if t == "Subquery Scan" || t == "CTE Scan" {
				// What's inside is another query level, named on its own.
				for _, c := range n.Plans {
					if c.Parent == "Subquery" {
						walk(c, parallel)
					}
				}
				return aliases, ok
			}
		case t == "Append" || t == "Merge Append":
			ok = false
		}
		for _, c := range n.Plans {
			if c.Parent == "InitPlan" || c.Parent == "SubPlan" {
				continue
			}
			a, cok := walk(c, parallel)
			aliases = append(aliases, a...)
			ok = ok && cok
		}
		if isJoin(t) && ok && len(aliases) >= 2 && n.PlanRows > 0 {
			loops := n.ActualLoops
			if loops <= 1 || parallel {
				f := n.ActualRows / n.PlanRows
				if n.ActualRows == 0 {
					f = 1 / n.PlanRows // as near nothing as an estimate goes
				}
				if f > RowsFactorMin || f < 1/RowsFactorMin {
					sorted := append([]string(nil), aliases...)
					sort.Strings(sorted)
					c := Correction{Aliases: sorted, Factor: round2(f)}
					if !seen[c.Key()] && !dupes(sorted) {
						seen[c.Key()] = true
						out = append(out, c)
					}
				}
			}
		}
		return aliases, ok
	}
	walkInner(root, walk)
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Aliases) != len(out[j].Aliases) {
			return len(out[i].Aliases) < len(out[j].Aliases)
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

// walkInner starts the walk, leaving out the inner side of nested loops,
// whose numbers are per outer row: a join under there is measured but not
// counted. The outer side and everything above are walked as usual.
func walkInner(root *Node, walk func(*Node, bool) ([]string, bool)) {
	var strip func(n *Node) *Node
	strip = func(n *Node) *Node {
		if n == nil {
			return nil
		}
		c := *n
		c.Plans = nil
		for i, ch := range n.Plans {
			s := strip(ch)
			if n.NodeType == "Nested Loop" && i == 1 {
				s = innerOnly(s)
			}
			c.Plans = append(c.Plans, s)
		}
		return &c
	}
	walk(strip(root), false)
}

// innerOnly keeps a nested loop's inner side for its aliases but takes away
// the numbers of every join in it, so none is corrected.
func innerOnly(n *Node) *Node {
	if n == nil {
		return nil
	}
	c := *n
	if isJoin(c.NodeType) {
		c.PlanRows = 0
	}
	c.Plans = nil
	for _, ch := range n.Plans {
		c.Plans = append(c.Plans, innerOnly(ch))
	}
	return &c
}

func isJoin(t string) bool {
	t = strings.TrimPrefix(t, "Parallel ")
	return t == "Nested Loop" || t == "Hash Join" || t == "Merge Join"
}

func dupes(sorted []string) bool {
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			return true
		}
	}
	return false
}

// round2 keeps two significant figures, so the same correction measured
// twice reads the same.
func round2(f float64) float64 {
	if f <= 0 {
		return f
	}
	p := math.Pow(10, math.Floor(math.Log10(f))-1)
	return math.Round(f/p) * p
}

// Merge adds the corrections in more whose joins aren't in base yet.
func Merge(base, more []Correction) []Correction {
	out := append([]Correction(nil), base...)
	seen := map[string]bool{}
	for _, c := range base {
		seen[c.Key()] = true
	}
	for _, c := range more {
		if !seen[c.Key()] {
			seen[c.Key()] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Aliases) != len(out[j].Aliases) {
			return len(out[i].Aliases) < len(out[j].Aliases)
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

// RowsHint writes corrections as pg_hint_plan Rows hints, such as
// "Rows(c o *8300) Rows(c l o *2.5)". Empty when there are none.
func RowsHint(cs []Correction) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, fmt.Sprintf("Rows(%s *%s)", c.Key(), strconv.FormatFloat(c.Factor, 'g', -1, 64)))
	}
	return strings.Join(parts, " ")
}
