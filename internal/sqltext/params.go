// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package sqltext

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// How a statement uses a $n parameter, for finding a value to run it with.
const (
	UseEqual  = "equal"  // compared for equality or a pattern: = <> != LIKE ILIKE, IN (...)
	UseLess   = "less"   // a one-sided range: < <= > >=
	UseLow    = "low"    // the lower end of BETWEEN
	UseHigh   = "high"   // the upper end of BETWEEN
	UseArray  = "array"  // = ANY ($n) or <> ALL ($n): a whole array
	UseLimit  = "limit"  // LIMIT $n, FETCH FIRST $n
	UseOffset = "offset" // OFFSET $n
	UseKey    = "key"    // a JSON key: x -> $n, x ->> $n
)

// ParamUse is how a statement uses one $n parameter.
type ParamUse struct {
	N         int
	Use       string // one of the Use constants; "" when LQO can't tell
	Expr      string // the expression it's compared with, as written
	Qualifier string // the alias or table the expression's columns name, "" when none
	List      int    // parameters of one IN (...) list share a number above 0
	Reason    string // why LQO can't find a value, when it can't
}

// Range is the table an alias stands for in a statement's FROM clause.
type Range struct {
	Table string // as written, schema-qualified when the text is; "" for a subquery or function
	Alias string // as written; the table's own name when it has no alias
}

// Aliases returns the FROM and JOIN items of a statement, at every depth:
// each alias, or the table's own name, with the table it stands for.
func Aliases(sql string) []Range {
	code := trimEnd(Code(sql))
	var out []Range
	seen := map[string]bool{}
	add := func(r Range) {
		k := strings.ToLower(r.Alias)
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, r)
	}
	inFrom := []bool{false}
	expect := false
	for i := 0; i < len(code); i++ {
		t := code[i]
		d := len(inFrom) - 1
		if t.Kind == Op {
			switch t.Text {
			case "(":
				if expect {
					// A subquery or a function in FROM: find its alias after
					// the closing parenthesis.
					j := matchParen(code, i)
					if j > 0 {
						if a, next := aliasAfter(code, j+1); a != "" {
							add(Range{Alias: a})
							_ = next
						}
					}
				}
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
			case "from", "join":
				inFrom[d], expect = true, true
				continue
			case "lateral", "only":
				continue
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
		name := t.Text
		last := identName(t)
		for i+2 < len(code) && code[i+1].Kind == Op && code[i+1].Text == "." && (code[i+2].Kind == Word || code[i+2].Kind == Ident) {
			name += "." + code[i+2].Text
			last = identName(code[i+2])
			i += 2
		}
		if i+1 < len(code) && code[i+1].Kind == Op && code[i+1].Text == "(" {
			// A function in FROM.
			expect = false
			continue
		}
		alias, next := aliasAfter(code, i+1)
		if alias == "" {
			alias = last
		} else {
			i = next - 1
		}
		add(Range{Table: name, Alias: alias})
		expect = false
	}
	return out
}

// notAlias are words that can follow a FROM item without being its alias.
var notAlias = map[string]bool{
	"where": true, "join": true, "inner": true, "left": true, "right": true, "full": true,
	"cross": true, "natural": true, "on": true, "using": true, "group": true, "order": true,
	"limit": true, "offset": true, "fetch": true, "union": true, "intersect": true,
	"except": true, "window": true, "having": true, "for": true, "tablesample": true,
	"returning": true, "outer": true, "lateral": true, "with": true, "select": true,
}

// aliasAfter reads an alias at code[i]: [AS] name. It returns the alias as
// PostgreSQL folds it and the index after it.
func aliasAfter(code []Token, i int) (string, int) {
	if i < len(code) && code[i].Kind == Word && strings.EqualFold(code[i].Text, "as") {
		i++
	}
	if i >= len(code) {
		return "", i
	}
	t := code[i]
	if t.Kind == Ident || (t.Kind == Word && !notAlias[strings.ToLower(t.Text)]) {
		return identName(t), i + 1
	}
	return "", i
}

// matchParen returns the index of the parenthesis that closes code[i].
func matchParen(code []Token, i int) int {
	depth := 0
	for j := i; j < len(code); j++ {
		if code[j].Kind != Op {
			continue
		}
		switch code[j].Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

var compareOps = map[string]string{
	"=": UseEqual, "<>": UseEqual, "!=": UseEqual, "~~": UseEqual, "~~*": UseEqual,
	"!~~": UseEqual, "!~~*": UseEqual,
	"<": UseLess, "<=": UseLess, ">": UseLess, ">=": UseLess,
}

// typeWords can name the type of a typed literal such as date '2026-01-01'.
var typeWords = map[string]bool{
	"date": true, "time": true, "timestamp": true, "timestamptz": true, "interval": true,
	"with": true, "without": true, "zone": true, "numeric": true, "int": true, "integer": true,
	"bigint": true, "smallint": true, "text": true, "varchar": true, "char": true, "boolean": true,
	"real": true, "double": true, "precision": true, "uuid": true, "json": true, "jsonb": true,
	"inet": true, "bytea": true,
}

// boundary words end an expression when LQO reads outwards from an operator.
var boundary = map[string]bool{
	"where": true, "and": true, "or": true, "on": true, "not": true, "when": true, "then": true,
	"else": true, "end": true, "having": true, "select": true, "by": true, "case": true,
	"between": true, "in": true, "like": true, "ilike": true, "similar": true, "is": true,
	"from": true, "join": true, "limit": true, "offset": true, "as": true, "using": true,
	"union": true, "intersect": true, "except": true, "group": true, "order": true,
	"exists": true, "any": true, "all": true, "some": true, "distinct": true, "asc": true,
	"desc": true, "nulls": true, "returning": true, "values": true, "fetch": true,
}

// ParamUses reads how a statement uses each $n parameter. A parameter used
// more than once is read from its first use.
func ParamUses(sql string) map[int]ParamUse {
	code := trimEnd(Code(sql))
	out := map[int]ParamUse{}
	for i, t := range code {
		if t.Kind != Param {
			continue
		}
		n, err := strconv.Atoi(t.Text[1:])
		if err != nil {
			continue
		}
		if _, ok := out[n]; ok {
			continue
		}
		u := paramUse(code, i)
		u.N = n
		out[n] = u
	}
	return out
}

func paramUse(code []Token, i int) ParamUse {
	word := func(j int, w ...string) bool {
		if j < 0 || j >= len(code) || code[j].Kind != Word {
			return false
		}
		for _, x := range w {
			if strings.EqualFold(code[j].Text, x) {
				return true
			}
		}
		return false
	}
	op := func(j int, o string) bool {
		return j >= 0 && j < len(code) && code[j].Kind == Op && code[j].Text == o
	}
	// The parameter and a cast after it: $1::int, $1::timestamp(3), $1::text[].
	end := skipCast(code, i+1)
	before := i - 1
	// A typed literal: date $1, interval $2, timestamp with time zone $3.
	// pg_stat_statements writes date '2026-01-01' that way.
	for before >= 1 && code[before].Kind == Word && !boundary[strings.ToLower(code[before].Text)] &&
		(typeWords[strings.ToLower(code[before].Text)] || code[before-1].Kind == Op) {
		if code[before-1].Kind == Op {
			if _, ok := compareOps[code[before-1].Text]; ok && typeWords[strings.ToLower(code[before].Text)] {
				before--
			}
			break
		}
		before--
	}

	switch {
	case word(before, "limit"):
		return ParamUse{Use: UseLimit}
	case word(before, "offset"):
		return ParamUse{Use: UseOffset}
	case word(before, "first", "next") && word(before-1, "fetch"):
		return ParamUse{Use: UseLimit}
	}

	// x->>$1: pg_stat_statements turns JSON keys into parameters too.
	if before >= 0 && code[before].Kind == Op && (code[before].Text == "->" || code[before].Text == "->>") {
		return withExpr(code, exprBefore(code, before), ParamUse{Use: UseKey})
	}

	// = ANY ($1) / <> ALL ($1)
	if op(before, "(") && op(end, ")") && word(before-1, "any", "all", "some") && before-2 >= 0 {
		if _, ok := compareOps[code[before-2].Text]; ok && code[before-2].Kind == Op {
			return withExpr(code, exprBefore(code, before-2), ParamUse{Use: UseArray})
		}
	}

	// x IN ($1, $2, ...): every item a parameter or a value.
	if op(before, "(") || op(before, ",") {
		open := before
		for open >= 0 && !op(open, "(") {
			open--
		}
		if open > 0 && word(open-1, "in") {
			close := matchParen(code, open)
			only := close > open
			for j := open + 1; only && j < close; j++ {
				k := code[j].Kind
				if !(k == Param || k == String || k == Number || op(j, ",") || op(j, "::") || (k == Word && j > 0 && op(j-1, "::"))) {
					only = false
				}
			}
			if only {
				in := open - 1
				if word(in-1, "not") {
					in--
				}
				u := withExpr(code, exprBefore(code, in), ParamUse{Use: UseEqual})
				u.List = open + 1 // one number per list: where it opens
				return u
			}
		}
	}

	// x BETWEEN $1 AND $2
	if word(before, "between") || (word(before, "symmetric") && word(before-1, "between")) {
		b := before
		if word(b, "symmetric") {
			b--
		}
		if word(b-1, "not") {
			b--
		}
		return withExpr(code, exprBefore(code, b), ParamUse{Use: UseLow})
	}
	if word(before, "and") {
		// Find BETWEEN ... AND $2 at the same depth.
		for j := before - 1; j >= 0; j-- {
			if code[j].Kind == Op && (code[j].Text == ")" || code[j].Text == "(") {
				break
			}
			if word(j, "between") {
				b := j
				if word(b-1, "not") {
					b--
				}
				return withExpr(code, exprBefore(code, b), ParamUse{Use: UseHigh})
			}
			if word(j, "and", "or", "where", "on", "when") {
				break
			}
		}
	}

	// x LIKE $1, x NOT ILIKE $1
	if word(before, "like", "ilike") {
		b := before
		if word(b-1, "not") {
			b--
		}
		return withExpr(code, exprBefore(code, b), ParamUse{Use: UseEqual})
	}

	// x OP $1
	if before >= 0 && code[before].Kind == Op {
		if use, ok := compareOps[code[before].Text]; ok {
			return withExpr(code, exprBefore(code, before), ParamUse{Use: use})
		}
	}
	// $1 OP x
	if end < len(code) && code[end].Kind == Op {
		if use, ok := compareOps[code[end].Text]; ok {
			return withExpr(code, exprAfter(code, end+1), ParamUse{Use: use})
		}
	}
	return ParamUse{Reason: "it isn't compared with a column"}
}

// skipCast returns the index after a cast that starts at code[i], if any.
func skipCast(code []Token, i int) int {
	for i < len(code) && code[i].Kind == Op && code[i].Text == "::" {
		i++
		for i < len(code) && (code[i].Kind == Word || code[i].Kind == Ident) {
			i++
			if i+1 < len(code) && code[i].Kind == Op && code[i].Text == "." {
				i++
			}
		}
		if i < len(code) && code[i].Kind == Op && code[i].Text == "(" {
			if j := matchParen(code, i); j > 0 {
				i = j + 1
			}
		}
		for i+1 < len(code) && code[i].Kind == Op && code[i].Text == "[" && code[i+1].Kind == Op && code[i+1].Text == "]" {
			i += 2
		}
		if i < len(code) && code[i].Kind == Op && code[i].Text == "[]" {
			i++
		}
	}
	return i
}

// exprBefore returns the tokens of the expression that ends just before
// code[i], read backwards until a boundary at its own depth.
func exprBefore(code []Token, i int) []Token {
	depth := 0
	j := i - 1
	for ; j >= 0; j-- {
		t := code[j]
		if t.Kind == Op {
			switch t.Text {
			case ")", "]":
				depth++
				continue
			case "(", "[":
				if depth == 0 {
					return code[j+1 : i]
				}
				depth--
				continue
			}
			if depth == 0 && stopOp(t.Text) {
				return code[j+1 : i]
			}
			continue
		}
		if depth == 0 && t.Kind == Word && boundary[strings.ToLower(t.Text)] {
			return code[j+1 : i]
		}
	}
	return code[:i]
}

// exprAfter returns the tokens of the expression that starts at code[i].
func exprAfter(code []Token, i int) []Token {
	depth := 0
	j := i
	for ; j < len(code); j++ {
		t := code[j]
		if t.Kind == Op {
			switch t.Text {
			case "(", "[":
				depth++
				continue
			case ")", "]":
				if depth == 0 {
					return code[i:j]
				}
				depth--
				continue
			}
			if depth == 0 && stopOp(t.Text) {
				return code[i:j]
			}
			continue
		}
		if depth == 0 && t.Kind == Word && boundary[strings.ToLower(t.Text)] {
			return code[i:j]
		}
	}
	return code[i:]
}

// stopOp reports whether an operator ends an expression: a comparison, a
// comma or a semicolon.
func stopOp(o string) bool {
	if _, ok := compareOps[o]; ok {
		return true
	}
	return o == "," || o == ";"
}

// withExpr fills in the expression a parameter is compared with, or the
// reason LQO can't sample values for it.
func withExpr(code []Token, expr []Token, u ParamUse) ParamUse {
	if len(expr) == 0 {
		return ParamUse{Reason: "it isn't compared with a column"}
	}
	quals := map[string]bool{}
	hasColumn := false
	for k, t := range expr {
		switch t.Kind {
		case Param:
			return ParamUse{Reason: "it's compared with an expression that has another parameter"}
		case Word:
			w := strings.ToLower(t.Text)
			if w == "select" {
				return ParamUse{Reason: "it's compared with a subquery"}
			}
		}
		if (t.Kind == Word || t.Kind == Ident) && k+2 < len(expr) && expr[k+1].Kind == Op && expr[k+1].Text == "." && (expr[k+2].Kind == Word || expr[k+2].Kind == Ident) {
			// q.col, but not schema.function(...)
			if !(k+3 < len(expr) && expr[k+3].Kind == Op && expr[k+3].Text == "(") {
				quals[identName(t)] = true
			}
			hasColumn = true
		}
		if t.Kind == Word || t.Kind == Ident {
			next := k+1 < len(expr) && expr[k+1].Kind == Op && expr[k+1].Text == "("
			prevCast := k > 0 && expr[k-1].Kind == Op && expr[k-1].Text == "::"
			if !next && !prevCast {
				hasColumn = true
			}
		}
	}
	if !hasColumn {
		return ParamUse{Reason: "it isn't compared with a column"}
	}
	if len(quals) > 1 {
		names := make([]string, 0, len(quals))
		for q := range quals {
			names = append(names, q)
		}
		sort.Strings(names)
		return ParamUse{Reason: fmt.Sprintf("it's compared with an expression over more than one table (%s)", strings.Join(names, ", "))}
	}
	var b strings.Builder
	for k, t := range expr {
		if k > 0 && spaced(expr[k-1], t) {
			b.WriteByte(' ')
		}
		b.WriteString(t.Text)
	}
	u.Expr = b.String()
	for q := range quals {
		u.Qualifier = q
	}
	return u
}

// spaced reports whether two tokens need a space between them when the
// expression is written out again.
func spaced(a, b Token) bool {
	if a.Kind == Op || b.Kind == Op {
		return false
	}
	return true
}
