// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Command compare puts two lqo advise JSON reports of the same queries side
// by side, shape by shape: what each suggested. It's how the recorded runs
// check that a workload read from pg_stat_statements reaches the same
// suggestions as the same workload read from a file.
//
//	go run ./scripts/compare FILE.json STATS.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/learned-query-optimizer/lqo/internal/advisor"
)

func load(path string) *advisor.Report {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var r advisor.Report
	if err := json.Unmarshal(b, &r); err != nil {
		fmt.Fprintln(os.Stderr, path+":", err)
		os.Exit(1)
	}
	return &r
}

func verdict(s *advisor.ShapeResult) string {
	if s == nil {
		return "not in this run"
	}
	switch s.Verdict {
	case advisor.Faster:
		return fmt.Sprintf("%q, −%.0f%%", s.Best.Name, s.Best.Gain*100)
	case advisor.Keep:
		return "keep the default plan"
	}
	r := s.Verdict
	if s.Reason != "" {
		r += ": " + s.Reason
	}
	return r
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: compare FILE.json STATS.json")
		os.Exit(2)
	}
	a, b := load(os.Args[1]), load(os.Args[2])
	byID := func(r *advisor.Report) map[string]*advisor.ShapeResult {
		m := map[string]*advisor.ShapeResult{}
		for i := range r.Shapes {
			m[r.Shapes[i].ID] = &r.Shapes[i]
		}
		return m
	}
	ma, mb := byID(a), byID(b)
	var ids []string
	for id, s := range ma {
		if s.Verdict == advisor.Skipped && mb[id] == nil {
			continue // a statement only the file has that LQO won't run
		}
		ids = append(ids, id)
	}
	for id := range mb {
		if ma[id] == nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	fmt.Printf("# %s against %s\n\n", strings.Join(a.Workload, ", "), strings.Join(b.Workload, ", "))
	fmt.Println("| Shape | Query | From the file | From pg_stat_statements | Same |")
	fmt.Println("|---|---|---|---|---|")
	fasterA, fasterB, same, both := 0, 0, 0, 0
	for _, id := range ids {
		sa, sb := ma[id], mb[id]
		ex := sb
		if sa != nil {
			ex = sa
		}
		q := strings.Join(strings.Fields(ex.Example), " ")
		if len(q) > 60 {
			q = q[:59] + "…"
		}
		eq := ""
		if sa != nil && sb != nil {
			both++
			ha, hb := "", ""
			if sa.Verdict == advisor.Faster {
				ha = sa.Best.Name
			}
			if sb.Verdict == advisor.Faster {
				hb = sb.Best.Name
			}
			if sa.Verdict == sb.Verdict && ha == hb {
				eq = "yes"
				same++
			} else {
				eq = "no"
			}
		}
		if sa != nil && sa.Verdict == advisor.Faster {
			fasterA++
		}
		if sb != nil && sb.Verdict == advisor.Faster {
			fasterB++
		}
		fmt.Printf("| `%s` | `%s` | %s | %s | %s |\n", id[:8], strings.ReplaceAll(q, "|", "\\|"), verdict(sa), verdict(sb), eq)
	}
	fmt.Printf("\nShapes in both runs: %d, with the same verdict and hint: %d.\n", both, same)
	fmt.Printf("Suggestions: %d from the file, %d from pg_stat_statements.\n", fasterA, fasterB)
}
