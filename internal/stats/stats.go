// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package stats reads a workload from pg_stat_statements. The view keeps
// each statement's shape with $n in place of its values, and its calls and
// time, so this package also finds values to run each shape with: it
// samples rows from the tables the parameters are compared with.
package stats

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/workload"
)

// Limit is how many entries are read, those with the most total time.
var Limit = 500

// Entry is one row of pg_stat_statements.
type Entry struct {
	QueryID string
	Query   string
	Calls   int
	TotalMs float64
}

// Summary counts what the read kept and why it dropped the rest.
type Summary struct {
	Read     int // entries read
	Kept     int // reads LQO may run
	NotReads int // writes, utility statements, LQO's own statements
	System   int // reads of the system catalogs only, such as drivers' and tools' own queries
	Hidden   int // <insufficient privilege>
	Version  string
}

func (s Summary) String() string {
	parts := []string{fmt.Sprintf("%d entries", s.Read), fmt.Sprintf("%d reads", s.Kept)}
	if s.NotReads > 0 {
		parts = append(parts, fmt.Sprintf("%d not reads", s.NotReads))
	}
	if s.System > 0 {
		parts = append(parts, fmt.Sprintf("%d on system catalogs only", s.System))
	}
	if s.Hidden > 0 {
		parts = append(parts, fmt.Sprintf("%d hidden from this user", s.Hidden))
	}
	return strings.Join(parts, ", ")
}

// ErrNotInstalled is returned when the database has no pg_stat_statements.
var ErrNotInstalled = errors.New("pg_stat_statements isn't installed in this database: it needs shared_preload_libraries = 'pg_stat_statements' and CREATE EXTENSION pg_stat_statements")

// Read reads the entries of the connected database with the most total
// time, top-level statements only where the view says.
func Read(ctx context.Context, conn *pgwire.Conn) ([]Entry, Summary, error) {
	var sum Summary
	res, err := conn.Exec(ctx, "SELECT quote_ident(n.nspname), e.extversion FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'pg_stat_statements'")
	if err != nil {
		return nil, sum, err
	}
	if len(res) == 0 || len(res[len(res)-1].Rows) == 0 {
		return nil, sum, ErrNotInstalled
	}
	row := res[len(res)-1].Rows[0]
	schema, version := deref(row[0]), deref(row[1])
	sum.Version = version
	total := "total_exec_time"
	if versionLess(version, "1.8") {
		total = "total_time"
	}
	where := "s.dbid = (SELECT oid FROM pg_database WHERE datname = current_database())"
	if !versionLess(version, "1.9") {
		where += " AND s.toplevel"
	}
	q := fmt.Sprintf("SELECT s.queryid::text, s.query, s.calls::text, s.%s::text FROM %s.pg_stat_statements s WHERE %s ORDER BY s.%s DESC LIMIT %d",
		total, schema, where, total, Limit)
	res, err = conn.Exec(ctx, q)
	if err != nil {
		var pe *pgwire.PgError
		if errors.As(err, &pe) && strings.Contains(pe.Message, "shared_preload_libraries") {
			return nil, sum, fmt.Errorf("%w (%s)", ErrNotInstalled, pe.Message)
		}
		return nil, sum, err
	}
	var out []Entry
	for _, r := range res[len(res)-1].Rows {
		sum.Read++
		e := Entry{QueryID: deref(r[0]), Query: deref(r[1])}
		e.Calls, _ = strconv.Atoi(deref(r[2]))
		e.TotalMs, _ = strconv.ParseFloat(deref(r[3]), 64)
		if e.Query == "<insufficient privilege>" || r[1] == nil {
			sum.Hidden++
			continue
		}
		if sqltext.ClassifyTemplate(e.Query) != "" || own(e.Query) {
			sum.NotReads++
			continue
		}
		if systemOnly(e.Query) {
			sum.System++
			continue
		}
		sum.Kept++
		out = append(out, e)
	}
	return out, sum, nil
}

// systemOnly reports a statement that reads no table, or only the system
// catalogs and statistics views: what drivers, tools and LQO itself ask.
func systemOnly(q string) bool {
	for _, t := range sqltext.Tables(q) {
		schema, name, ok := strings.Cut(t, ".")
		if !ok {
			name, schema = schema, ""
		}
		switch {
		case schema == "pg_catalog" || schema == "information_schema":
		case schema == "" && strings.HasPrefix(name, "pg_"):
		default:
			return false
		}
	}
	return true
}

// own reports a statement LQO itself sends: its checks of settings.
func own(q string) bool {
	n := sqltext.Normalize(q)
	return strings.Contains(n, "current_setting") && strings.Contains(n, "lqo") ||
		strings.HasPrefix(n, "select set_config")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// versionLess compares extension versions such as 1.10 and 1.9.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// Defaults for parameters that aren't compared with a column.
const (
	LimitValue  = "100"
	OffsetValue = "0"
)

// sampleRows is about how many rows LQO samples from a table.
const sampleRows = 2000

// Filler finds values for the parameters of statements, from rows of the
// tables they're compared with. It runs every sample in a READ ONLY
// transaction that it rolls back, on the server the trials run on.
type Filler struct {
	Conn    *pgwire.Conn
	Samples int           // distinct texts wanted per statement
	Timeout time.Duration // per sampling statement
}

// Statements turns entries into workload statements: for each entry, up to
// Samples texts with values, the first carrying the entry's calls and time.
// An entry LQO can't find values for becomes one statement with its reason.
func (f *Filler) Statements(ctx context.Context, entries []Entry) ([]workload.Statement, error) {
	var out []workload.Statement
	for _, e := range entries {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		src := "pg_stat_statements"
		if e.QueryID != "" {
			src += " " + e.QueryID
		}
		texts, why, err := f.Fill(ctx, e.Query)
		if err != nil {
			return out, err
		}
		base := workload.Statement{Source: src, QueryID: e.QueryID, Calls: e.Calls, LoggedMs: e.TotalMs, Totals: true, Filled: true}
		if len(texts) == 0 {
			base.Text = e.Query
			base.Skip = why
			out = append(out, base)
			continue
		}
		for i, t := range texts {
			st := base
			st.Text = t
			if i > 0 {
				st.Totals, st.NoCount, st.Calls, st.LoggedMs = false, true, 0, -1
			}
			out = append(out, st)
		}
	}
	return out, nil
}

// Fill returns up to Samples texts of query with its parameters replaced by
// values, or the reason it can't. A query with no parameters comes back as
// it is.
func (f *Filler) Fill(ctx context.Context, query string) ([]string, string, error) {
	uses := sqltext.ParamUses(query)
	if len(uses) == 0 {
		return []string{query}, "", nil
	}
	want := max(1, f.Samples)
	// JSON keys first: the values compared with x->>$1 depend on the key.
	var keys []int
	for n, u := range uses {
		if u.Use == sqltext.UseKey {
			keys = append(keys, n)
		}
	}
	if len(keys) == 0 {
		return f.fill(ctx, query, uses, want, 0)
	}
	sort.Ints(keys)
	variants, why, err := f.keyVariants(ctx, query, uses, keys, want)
	if err != nil || why != "" {
		return nil, why, err
	}
	var out []string
	seen := map[string]bool{}
	per := (want + len(variants) - 1) / len(variants)
	for k, v := range variants {
		texts, why2, err := f.fill(ctx, v, sqltext.ParamUses(v), per, k)
		if err != nil {
			return nil, "", err
		}
		if why == "" {
			why = why2
		}
		for _, t := range texts {
			if !seen[t] && len(out) < want {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	if len(out) > 0 {
		why = ""
	}
	return out, why, nil
}

// keyVariants fills the JSON key parameters with the keys the rows have,
// the most frequent first. Key parameters on the same expression take
// different keys, so x->>$1 = $2 AND x->>$3 = $4 reads two keys.
func (f *Filler) keyVariants(ctx context.Context, query string, uses map[int]sqltext.ParamUse, keys []int, want int) ([]string, string, error) {
	ranges := sqltext.Aliases(query)
	type keyGroup struct {
		ns   []int
		keys []string
	}
	groups := map[string]*keyGroup{}
	var order []string
	for _, n := range keys {
		u := uses[n]
		if u.Reason != "" {
			return nil, fmt.Sprintf("LQO can't choose a value for $%d: %s", n, u.Reason), nil
		}
		g := groups[u.Expr]
		if g == nil {
			candidates, why := candidatesFor(u, ranges)
			if why != "" {
				return nil, fmt.Sprintf("LQO can't choose a value for $%d: %s", n, why), nil
			}
			r, err := f.pick(ctx, u.Expr, candidates)
			if err != nil {
				return nil, "", err
			}
			if r == nil {
				return nil, fmt.Sprintf("LQO can't choose a value for $%d: no table here has %s", n, u.Expr), nil
			}
			ks, err := f.jsonKeys(ctx, *r, u.Expr)
			if err != nil {
				return nil, "", err
			}
			if len(ks) == 0 {
				return nil, fmt.Sprintf("LQO couldn't take JSON keys for $%d from %s", n, r.Table), nil
			}
			g = &keyGroup{keys: ks}
			groups[u.Expr] = g
			order = append(order, u.Expr)
		}
		g.ns = append(g.ns, n)
	}
	var out []string
	seen := map[string]bool{}
	for i := 0; i < want; i++ {
		params := map[int]string{}
		for _, e := range order {
			g := groups[e]
			for j, n := range g.ns {
				params[n] = literal(g.keys[(i+j)%len(g.keys)])
			}
		}
		// Substitute only the keys; other parameters stay for the next step.
		v := sqltext.Substitute(query, params)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, "", nil
}

// jsonKeys returns the keys of the JSON objects in expr, the most frequent
// first, from sampled rows.
func (f *Filler) jsonKeys(ctx context.Context, r sqltext.Range, expr string) ([]string, error) {
	for _, sample := range []bool{true, false} {
		item, err := f.from(ctx, r, sample)
		if err != nil {
			return nil, err
		}
		q := fmt.Sprintf("SELECT k FROM (SELECT (%s)::jsonb AS j FROM %s WHERE (%s) IS NOT NULL LIMIT %d) s, jsonb_object_keys(CASE WHEN jsonb_typeof(s.j) = 'object' THEN s.j END) k GROUP BY k ORDER BY count(*) DESC, k LIMIT 20",
			expr, item, expr, sampleRows)
		rows, err := f.query(ctx, q)
		if err != nil {
			var pe *pgwire.PgError
			if errors.As(err, &pe) {
				return nil, nil
			}
			return nil, err
		}
		if len(rows) > 0 {
			var ks []string
			for _, row := range rows {
				ks = append(ks, row[0])
			}
			return ks, nil
		}
	}
	return nil, nil
}

// fill finds values for every parameter of a query that has no JSON key
// parameters left.
//
// Values take turns between the most frequent combinations and the rarest
// the sample has: a planner fooled by a rare value is as common as one
// fooled by a frequent one. start shifts where the turns begin, so that
// each JSON key variant doesn't get the same rank.
func (f *Filler) fill(ctx context.Context, query string, uses map[int]sqltext.ParamUse, want, start int) ([]string, string, error) {
	if len(uses) == 0 {
		return []string{query}, "", nil
	}
	ns := make([]int, 0, len(uses))
	for n, u := range uses {
		if u.Reason != "" {
			return nil, fmt.Sprintf("LQO can't choose a value for $%d: %s", n, u.Reason), nil
		}
		ns = append(ns, n)
	}
	sort.Ints(ns)
	ranges := sqltext.Aliases(query)

	// Group the compared parameters by the FROM item their expression reads.
	type group struct {
		rng   sqltext.Range
		equal []int // parameters sampled together, row by row
		other []int // ranges and arrays
	}
	var groups []*group
	byAlias := map[string]*group{}
	values := map[int][]string{} // per parameter, one value per text
	fixed := map[int]string{}
	for _, n := range ns {
		u := uses[n]
		switch u.Use {
		case sqltext.UseKey:
			return nil, fmt.Sprintf("LQO can't choose a value for $%d: a JSON key inside another expression", n), nil
		case sqltext.UseLimit:
			fixed[n] = LimitValue
			continue
		case sqltext.UseOffset:
			fixed[n] = OffsetValue
			continue
		}
		candidates, why := candidatesFor(u, ranges)
		if why != "" {
			return nil, fmt.Sprintf("LQO can't choose a value for $%d: %s", n, why), nil
		}
		// An unqualified expression in a query over several tables: the
		// first table where it reads is the one it means.
		r, err := f.pick(ctx, u.Expr, candidates)
		if err != nil {
			return nil, "", err
		}
		if r == nil {
			return nil, fmt.Sprintf("LQO can't choose a value for $%d: no table here has %s", n, u.Expr), nil
		}
		key := strings.ToLower(r.Alias)
		g := byAlias[key]
		if g == nil {
			g = &group{rng: *r}
			byAlias[key] = g
			groups = append(groups, g)
		}
		if u.Use == sqltext.UseEqual {
			g.equal = append(g.equal, n)
		} else {
			g.other = append(g.other, n)
		}
	}

	for _, g := range groups {
		if len(g.equal) > 0 {
			tuples, err := f.tuples(ctx, g.rng, exprs(uses, g.equal), 1000)
			if err != nil {
				return nil, "", err
			}
			if len(tuples) == 0 {
				return nil, fmt.Sprintf("LQO couldn't take values for $%d from %s", g.equal[0], g.rng.Table), nil
			}
			assignEqual(uses, g.equal, alternate(tuples), want, start, values)
		}
		for _, n := range g.other {
			u := uses[n]
			v, err := f.other(ctx, g.rng, u, want)
			if err != nil {
				return nil, "", err
			}
			if len(v) == 0 {
				return nil, fmt.Sprintf("LQO couldn't take values for $%d from %s", n, g.rng.Table), nil
			}
			values[n] = v
		}
	}

	var out []string
	seen := map[string]bool{}
	for i := 0; i < want; i++ {
		params := map[int]string{}
		for _, n := range ns {
			if v, ok := fixed[n]; ok {
				params[n] = v
				continue
			}
			vs := values[n]
			params[n] = vs[i%len(vs)]
		}
		t := sqltext.Substitute(query, params)
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, "", nil
}

// candidatesFor returns the FROM items an expression may read.
func candidatesFor(u sqltext.ParamUse, ranges []sqltext.Range) ([]sqltext.Range, string) {
	var tables []sqltext.Range
	for _, r := range ranges {
		if r.Table != "" {
			tables = append(tables, r)
		}
	}
	if u.Qualifier == "" {
		if len(tables) == 0 {
			return nil, "it isn't compared with a column of a table"
		}
		return tables, ""
	}
	for _, r := range ranges {
		if strings.EqualFold(r.Alias, u.Qualifier) {
			if r.Table == "" {
				return nil, fmt.Sprintf("it's compared with a column of %s, which isn't a table", u.Qualifier)
			}
			return []sqltext.Range{r}, ""
		}
	}
	return nil, fmt.Sprintf("LQO can't tell which table %s is", u.Qualifier)
}

func exprs(uses map[int]sqltext.ParamUse, ns []int) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = uses[n].Expr
	}
	return out
}

// assignEqual gives each equality parameter its values, one per text: the
// most frequent combinations first. Parameters of one IN list take
// different values from successive combinations.
func assignEqual(uses map[int]sqltext.ParamUse, ns []int, tuples [][]string, want, start int, values map[int][]string) {
	pos := map[int]int{} // list -> how many of its parameters came before
	for col, n := range ns {
		u := uses[n]
		offset := 0
		if u.List > 0 {
			offset = pos[u.List]
			pos[u.List]++
		}
		var vs []string
		for i := 0; i < want; i++ {
			t := tuples[(i+start+offset)%len(tuples)]
			vs = append(vs, literal(t[col]))
		}
		values[n] = vs
	}
}

// alternate reorders combinations from most to least frequent as the most
// frequent, the rarest, the second most frequent, the second rarest, ...
func alternate(tuples [][]string) [][]string {
	out := make([][]string, 0, len(tuples))
	for i, j := 0, len(tuples)-1; i <= j; i, j = i+1, j-1 {
		out = append(out, tuples[i])
		if i != j {
			out = append(out, tuples[j])
		}
	}
	return out
}

// literal writes a value as an untyped string literal, so the type it's
// compared with decides how it's read.
func literal(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// arrayLiteral writes values as an untyped array literal: '{"a","b"}'.
func arrayLiteral(vs []string) string {
	q := make([]string, len(vs))
	for i, v := range vs {
		v = strings.ReplaceAll(v, `\`, `\\`)
		v = strings.ReplaceAll(v, `"`, `\"`)
		q[i] = `"` + v + `"`
	}
	return literal("{" + strings.Join(q, ",") + "}")
}

// from writes the FROM item of a sampling statement, with TABLESAMPLE when
// the table is big enough for it to help.
func (f *Filler) from(ctx context.Context, r sqltext.Range, sample bool) (string, error) {
	item := r.Table + " AS " + quoteIdent(r.Alias)
	if !sample {
		return item, nil
	}
	v, err := f.query(ctx, "SELECT c.relkind::text || ' ' || c.reltuples::bigint FROM pg_class c WHERE c.oid = to_regclass("+literal(r.Table)+")")
	if err != nil || len(v) == 0 || len(v[0]) == 0 {
		return item, nil
	}
	kind, n, _ := strings.Cut(v[0][0], " ")
	tuples, _ := strconv.ParseFloat(n, 64)
	if (kind != "r" && kind != "m" && kind != "p") || tuples <= sampleRows*2 {
		return item, nil
	}
	pct := 100 * sampleRows * 2 / tuples
	return fmt.Sprintf("%s TABLESAMPLE SYSTEM (%.6f)", item, pct), nil
}

// pick returns the first of the candidates where expr reads, or nil.
func (f *Filler) pick(ctx context.Context, expr string, candidates []sqltext.Range) (*sqltext.Range, error) {
	for i := range candidates {
		q := "SELECT " + expr + " FROM " + candidates[i].Table + " AS " + quoteIdent(candidates[i].Alias) + " LIMIT 0"
		if _, err := f.query(ctx, q); err != nil {
			var pe *pgwire.PgError
			if errors.As(err, &pe) {
				continue
			}
			return nil, err
		}
		return &candidates[i], nil
	}
	return nil, nil
}

// tuples samples rows of r and returns the distinct combinations of exprs,
// none NULL, the most frequent first.
func (f *Filler) tuples(ctx context.Context, r sqltext.Range, ex []string, limit int) ([][]string, error) {
	var conds []string
	for _, e := range ex {
		conds = append(conds, "("+e+") IS NOT NULL")
	}
	cols := make([]string, len(ex))
	for i, e := range ex {
		cols[i] = "(" + e + ")::text"
	}
	for _, sample := range []bool{true, false} {
		item, err := f.from(ctx, r, sample)
		if err != nil {
			return nil, err
		}
		q := fmt.Sprintf("SELECT %s FROM (SELECT %s FROM %s WHERE %s LIMIT %d) s(%s) GROUP BY %s ORDER BY count(*) DESC, 1 LIMIT %d",
			colList(len(ex)), strings.Join(cols, ", "), item, strings.Join(conds, " AND "), sampleRows, colList(len(ex)), colList(len(ex)), limit)
		rows, err := f.query(ctx, q)
		if err != nil {
			var pe *pgwire.PgError
			if errors.As(err, &pe) {
				return nil, nil
			}
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}
	}
	return nil, nil
}

// other finds values for a range or array parameter: quantiles of a sample
// for ranges, the most frequent values for an array.
func (f *Filler) other(ctx context.Context, r sqltext.Range, u sqltext.ParamUse, want int) ([]string, error) {
	for _, sample := range []bool{true, false} {
		item, err := f.from(ctx, r, sample)
		if err != nil {
			return nil, err
		}
		inner := fmt.Sprintf("SELECT (%s) AS v FROM %s WHERE (%s) IS NOT NULL LIMIT %d", u.Expr, item, u.Expr, sampleRows)
		var q string
		switch u.Use {
		case sqltext.UseArray:
			q = fmt.Sprintf("SELECT v::text FROM (%s) s GROUP BY v ORDER BY count(*) DESC, 1 LIMIT 3", inner)
		default:
			p := "0.5"
			switch u.Use {
			case sqltext.UseLow:
				p = "0.25"
			case sqltext.UseHigh:
				p = "0.75"
			}
			q = fmt.Sprintf("SELECT (percentile_disc(%s) WITHIN GROUP (ORDER BY v))::text FROM (%s) s HAVING count(*) > 0", p, inner)
		}
		rows, err := f.query(ctx, q)
		if err != nil {
			var pe *pgwire.PgError
			if errors.As(err, &pe) {
				return nil, nil
			}
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		if u.Use == sqltext.UseArray {
			var vs []string
			for _, row := range rows {
				vs = append(vs, row[0])
			}
			return []string{arrayLiteral(vs)}, nil
		}
		return []string{literal(rows[0][0])}, nil
	}
	return nil, nil
}

// query runs one statement in a READ ONLY transaction with a statement
// timeout, rolls it back, and returns its rows as text (NULL as "").
func (f *Filler) query(ctx context.Context, q string) ([][]string, error) {
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	sql := fmt.Sprintf("BEGIN TRANSACTION READ ONLY; SET LOCAL statement_timeout = %d; %s", timeout.Milliseconds(), q)
	cctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	res, err := f.Conn.Exec(cctx, sql)
	if _, rerr := f.Conn.Exec(ctx, "ROLLBACK"); rerr != nil && err == nil {
		err = rerr
	}
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, nil
	}
	var out [][]string
	for _, r := range res[len(res)-1].Rows {
		row := make([]string, len(r))
		for i, v := range r {
			row[i] = deref(v)
		}
		out = append(out, row)
	}
	return out, nil
}

func colList(n int) string {
	c := make([]string, n)
	for i := range c {
		c[i] = fmt.Sprintf("c%d", i+1)
	}
	return strings.Join(c, ", ")
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
