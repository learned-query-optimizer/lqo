// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package sqltext

import (
	"strings"
	"testing"
)

func TestTables(t *testing.T) {
	cases := []struct{ sql, want string }{
		{"SELECT 1", ""},
		{"SELECT * FROM t", "t"},
		{"select * from Public.T x where a = 1", "public.t"},
		{`SELECT * FROM "Mixed"."Case" m`, "Mixed.Case"},
		{"SELECT sum(l.qty * p.price) FROM lqo_demo.customers c JOIN lqo_demo.orders o ON o.customer_id = c.id JOIN lqo_demo.order_lines l ON l.order_id = o.id JOIN lqo_demo.products p ON p.id = l.product_id WHERE c.attrs->>'tier' = 'gold'",
			"lqo_demo.customers,lqo_demo.order_lines,lqo_demo.orders,lqo_demo.products"},
		// The Join Order Benchmark's style: a comma list.
		{"SELECT MIN(t.title) FROM company_type AS ct, info_type AS it, movie_companies AS mc, title AS t WHERE ct.id = mc.company_type_id AND t.production_year > 2005",
			"company_type,info_type,movie_companies,title"},
		{"SELECT * FROM a LEFT JOIN b USING (id, k) CROSS JOIN LATERAL (SELECT * FROM c WHERE c.id = a.id) s", "a,b,c"},
		{"WITH x AS (SELECT * FROM t1) SELECT * FROM x, t2 WHERE x.id = t2.id ORDER BY 1", "t1,t2,x"},
		{"SELECT (SELECT max(v) FROM u), count(*) FROM t GROUP BY 1", "t,u"},
		{"SELECT * FROM ONLY parent p", "parent"},
		{"SELECT * FROM t WHERE id IN (SELECT id FROM s) AND x = 1", "s,t"},
		{"SELECT generate_series(1, 3)", ""},
	}
	for _, c := range cases {
		if got := strings.Join(Tables(c.sql), ","); got != c.want {
			t.Errorf("Tables(%q) = %q, want %q", c.sql, got, c.want)
		}
	}
	// Shapes that differ in what they select and filter on, but read the
	// same tables, are one family; values and spelling don't matter.
	a := Family("SELECT count(*) FROM orders o JOIN customers c ON c.id = o.customer_id WHERE c.region = 3")
	b := Family("select sum(o.total) from customers c join orders o on o.customer_id = c.id where c.attrs->>'tier' = 'gold'")
	if a != b || a != "customers,orders" {
		t.Errorf("families %q and %q", a, b)
	}
}
