// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package sqltext

import (
	"reflect"
	"testing"
)

func TestAliases(t *testing.T) {
	cases := []struct {
		sql  string
		want []Range
	}{
		{"SELECT 1 FROM orders o JOIN customers AS c ON c.id = o.customer_id WHERE o.id = $1",
			[]Range{{"orders", "o"}, {"customers", "c"}}},
		{"SELECT * FROM lqo_demo.customers WHERE id = $1", []Range{{"lqo_demo.customers", "customers"}}},
		{`SELECT * FROM "Order Lines" ol, items i WHERE ol.x = i.y`, []Range{{`"Order Lines"`, "ol"}, {"items", "i"}}},
		{"SELECT * FROM (SELECT * FROM t) AS s WHERE s.a = $1", []Range{{"", "s"}, {"t", "t"}}},
		{"SELECT * FROM t WHERE a = $1 ORDER BY b LIMIT $2", []Range{{"t", "t"}}},
		{"SELECT * FROM t LEFT JOIN u ON true", []Range{{"t", "t"}, {"u", "u"}}},
	}
	for _, c := range cases {
		if got := Aliases(c.sql); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s\n got %+v\nwant %+v", c.sql, got, c.want)
		}
	}
}

func TestParamUses(t *testing.T) {
	type u = ParamUse
	cases := []struct {
		sql  string
		want map[int]ParamUse
	}{
		{"SELECT * FROM t WHERE a = $1 AND $2 < t.b", map[int]ParamUse{
			1: {N: 1, Use: UseEqual, Expr: "a"},
			2: {N: 2, Use: UseLess, Expr: "t.b", Qualifier: "t"},
		}},
		{"SELECT * FROM lqo_demo.customers c WHERE c.attrs->>'tier' = $1 AND c.attrs->>'channel' = $2::text", map[int]ParamUse{
			1: {N: 1, Use: UseEqual, Expr: "c.attrs->>'tier'", Qualifier: "c"},
			2: {N: 2, Use: UseEqual, Expr: "c.attrs->>'channel'", Qualifier: "c"},
		}},
		{"SELECT * FROM t WHERE x IN ($1, $2) AND y NOT IN ($3)", map[int]ParamUse{
			1: {N: 1, Use: UseEqual, Expr: "x", List: 8},
			2: {N: 2, Use: UseEqual, Expr: "x", List: 8},
			3: {N: 3, Use: UseEqual, Expr: "y", List: 17},
		}},
		{"SELECT * FROM t WHERE d BETWEEN $1 AND $2 LIMIT $3 OFFSET $4", map[int]ParamUse{
			1: {N: 1, Use: UseLow, Expr: "d"},
			2: {N: 2, Use: UseHigh, Expr: "d"},
			3: {N: 3, Use: UseLimit},
			4: {N: 4, Use: UseOffset},
		}},
		{"SELECT * FROM t WHERE id = ANY($1::int[]) AND name ILIKE $2 FETCH FIRST $3 ROWS ONLY", map[int]ParamUse{
			1: {N: 1, Use: UseArray, Expr: "id"},
			2: {N: 2, Use: UseEqual, Expr: "name"},
			3: {N: 3, Use: UseLimit},
		}},
		{"SELECT lower(t.name) FROM t WHERE lower(t.name) = $1", map[int]ParamUse{
			1: {N: 1, Use: UseEqual, Expr: "lower(t.name)", Qualifier: "t"},
		}},
		{"SELECT $1, a FROM t", map[int]ParamUse{1: {N: 1, Reason: "it isn't compared with a column"}}},
		{"SELECT * FROM t WHERE created > now() - $1::interval", map[int]ParamUse{1: {N: 1, Reason: "it isn't compared with a column"}}},
		{"SELECT * FROM t, u WHERE t.a + u.b = $1", map[int]ParamUse{1: {N: 1, Reason: "it's compared with an expression over more than one table (t, u)"}}},
		{"SELECT * FROM t WHERE a = (SELECT max(b) FROM u) + $1", map[int]ParamUse{1: {N: 1, Reason: "it isn't compared with a column"}}},
		{"SELECT * FROM t WHERE d > date $1 AND e < timestamp with time zone $2", map[int]ParamUse{
			1: {N: 1, Use: UseLess, Expr: "d"},
			2: {N: 2, Use: UseLess, Expr: "e"},
		}},
		{"SELECT * FROM t c WHERE c.attrs->>$1 = $2", map[int]ParamUse{
			1: {N: 1, Use: UseKey, Expr: "c.attrs", Qualifier: "c"},
			2: {N: 2, Reason: "it's compared with an expression that has another parameter"},
		}},
		{"SELECT * FROM t WHERE (a = $1 OR b = $1)", map[int]ParamUse{1: {N: 1, Use: UseEqual, Expr: "a"}}},
	}
	for _, c := range cases {
		got := ParamUses(c.sql)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s\n got %+v\nwant %+v", c.sql, got, c.want)
		}
	}
	_ = u{}
}
