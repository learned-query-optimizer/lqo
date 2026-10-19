// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package sqltext

import (
	"sort"
	"strings"
)

// Tables returns the tables a statement reads, as written in its FROM and
// JOIN clauses (schema-qualified when the text qualifies them), sorted and
// without repeats. Names inside subqueries count too, and so do the names
// of WITH queries. It reads the text only: a view counts as one table, and
// a name that search_path resolves differently is still the same name.
func Tables(sql string) []string {
	code := trimEnd(Code(sql))
	seen := map[string]bool{}
	inFrom := []bool{false} // per parenthesis depth
	expect := false
	for i := 0; i < len(code); i++ {
		t := code[i]
		d := len(inFrom) - 1
		if t.Kind == Op {
			switch t.Text {
			case "(":
				inFrom = append(inFrom, false)
				expect = false
			case ")":
				if d > 0 {
					inFrom = inFrom[:d]
				}
				expect = false
			case ",":
				expect = inFrom[d]
			default:
				expect = false
			}
			continue
		}
		if t.Kind != Word && t.Kind != Ident {
			expect = false
			continue
		}
		w := strings.ToLower(t.Text)
		if t.Kind == Word {
			switch w {
			case "from":
				inFrom[d], expect = true, true
				continue
			case "join":
				inFrom[d], expect = true, true
				continue
			case "lateral", "only":
				continue // still expecting the table
			case "where", "group", "order", "limit", "having", "union", "intersect", "except",
				"window", "offset", "fetch", "for", "returning", "on", "using", "select", "set", "values":
				if w != "on" && w != "using" {
					inFrom[d] = false
				}
				expect = false
				continue
			}
		}
		if !expect {
			continue
		}
		name := identName(t)
		for i+2 < len(code) && code[i+1].Kind == Op && code[i+1].Text == "." && (code[i+2].Kind == Word || code[i+2].Kind == Ident) {
			name += "." + identName(code[i+2])
			i += 2
		}
		seen[name] = true
		expect = false
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// identName is an identifier as PostgreSQL folds it: plain words in lower
// case, quoted ones as written.
func identName(t Token) string {
	if t.Kind == Ident {
		return strings.ReplaceAll(strings.Trim(t.Text, `"`), `""`, `"`)
	}
	return strings.ToLower(t.Text)
}

// Family is the key LQO groups related shapes by: the tables they read. It
// is empty for a statement that reads no table.
func Family(sql string) string {
	return strings.Join(Tables(sql), ",")
}
