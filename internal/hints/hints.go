// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package hints holds the planner settings LQO tries. Each Arm is a set of
// PostgreSQL's own planner switches, applied with SET LOCAL inside one
// transaction, so nothing leaks into the session or the server.
package hints

import (
	"fmt"
	"sort"
	"strings"
)

// Setting is one planner setting and the value to try.
type Setting struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Arm is one combination of settings to try against the server's default
// plan. An arm built for one shape from what its runs measured may instead
// carry Comment: pg_hint_plan hints, such as "Rows(c o *8300)", that go in a
// /*+ ... */ comment at the start of the query.
type Arm struct {
	Name     string
	Settings []Setting
	Comment  string
}

// HintComment is the arm's pg_hint_plan comment, with a space after it, or
// nothing.
func (a Arm) HintComment() string {
	if a.Comment == "" {
		return ""
	}
	return "/*+ " + a.Comment + " */ "
}

// Arm names for the hints LQO builds from measured row counts.
const (
	RowCounts      = "row counts"
	RowCountsTwice = "row counts, second pass"
)

// SetLocal is the arm as SET LOCAL statements, one per line.
func (a Arm) SetLocal() []string {
	out := make([]string, len(a.Settings))
	for i, s := range a.Settings {
		out[i] = fmt.Sprintf("SET LOCAL %s = %s;", s.Name, s.Value)
	}
	return out
}

// Names are the setting names the arm changes.
func (a Arm) Names() []string {
	out := make([]string, len(a.Settings))
	for i, s := range a.Settings {
		out[i] = s.Name
	}
	return out
}

func off(names ...string) []Setting {
	out := make([]Setting, len(names))
	for i, n := range names {
		out[i] = Setting{n, "off"}
	}
	return out
}

// Standard is the default set: thirteen arms that each switch off one
// join method, scan method or planner feature, or a few that commonly go
// together, plus one that keeps the join order as written.
func Standard() []Arm {
	return []Arm{
		{Name: "no nested loop", Settings: off("enable_nestloop")},
		{Name: "no hash join", Settings: off("enable_hashjoin")},
		{Name: "no merge join", Settings: off("enable_mergejoin")},
		{Name: "hash join only", Settings: off("enable_nestloop", "enable_mergejoin")},
		{Name: "nested loop only", Settings: off("enable_hashjoin", "enable_mergejoin")},
		{Name: "merge join only", Settings: off("enable_nestloop", "enable_hashjoin")},
		{Name: "no sequential scan", Settings: off("enable_seqscan")},
		{Name: "no index scan", Settings: off("enable_indexscan", "enable_indexonlyscan")},
		{Name: "no bitmap scan", Settings: off("enable_bitmapscan")},
		{Name: "no nested loop, no sequential scan", Settings: off("enable_nestloop", "enable_seqscan")},
		{Name: "no memoize", Settings: off("enable_memoize")},
		{Name: "no parallel workers", Settings: []Setting{{"max_parallel_workers_per_gather", "0"}}},
		{Name: "join order as written", Settings: []Setting{{"join_collapse_limit", "1"}, {"from_collapse_limit", "1"}}},
	}
}

// Full is the 48 arms the Bao research used: every combination of the
// three join methods and three scan methods that leaves at least one of
// each switched on, other than all on.
func Full() []Arm {
	joins := []string{"enable_hashjoin", "enable_mergejoin", "enable_nestloop"}
	scans := []string{"enable_indexscan", "enable_seqscan", "enable_indexonlyscan"}
	var arms []Arm
	for j := 1; j < 8; j++ {
		for s := 1; s < 8; s++ {
			if j == 7 && s == 7 {
				continue
			}
			var offNames []string
			for i, n := range joins {
				if j&(1<<i) == 0 {
					offNames = append(offNames, n)
				}
			}
			for i, n := range scans {
				if s&(1<<i) == 0 {
					offNames = append(offNames, n)
				}
			}
			arms = append(arms, Arm{Name: "off: " + strings.Join(short(offNames), ", "), Settings: off(offNames...)})
		}
	}
	return arms
}

func short(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = strings.TrimPrefix(n, "enable_")
	}
	return out
}

// Set returns the named hint set: standard or full.
func Set(name string) ([]Arm, error) {
	switch name {
	case "", "standard":
		return Standard(), nil
	case "full":
		return Full(), nil
	}
	return nil, fmt.Errorf("unknown hint set %q (use standard or full)", name)
}

// SettingNames lists every setting a set of arms uses, sorted.
func SettingNames(arms []Arm) []string {
	seen := map[string]bool{}
	for _, a := range arms {
		for _, s := range a.Settings {
			seen[s.Name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Supported keeps the arms whose settings the server has; known lists the
// server's setting names. An arm with a setting the server lacks (such as
// enable_memoize before PostgreSQL 14) is dropped whole.
func Supported(arms []Arm, known map[string]bool) (kept []Arm, dropped []string) {
	for _, a := range arms {
		ok := true
		for _, s := range a.Settings {
			if !known[s.Name] {
				ok = false
				break
			}
		}
		if ok {
			kept = append(kept, a)
		} else {
			dropped = append(dropped, a.Name)
		}
	}
	return kept, dropped
}
