// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package demo builds a small shop database in its own schema, lqo_demo,
// with data that fools PostgreSQL's row estimates in two well-known ways,
// and a workload to try LQO on. The data is the same on every run: it is
// made from generate_series with arithmetic, not random numbers.
//
// The traps are real ones. Customer attributes live in a JSONB column, and
// the planner has no statistics for values inside JSON, so it guesses that
// any one value matches 0.5% of rows. Two such conditions together it
// guesses at 0.0025%, when in this data they match half the customers. A
// plan built for a handful of customers then runs for a hundred thousand.
package demo

import (
	"context"
	"fmt"
	"strings"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
)

// Schema is the schema the demo creates and replaces.
const Schema = "lqo_demo"

// Sizes are the row counts at scale 1.
const (
	Customers  = 200000
	Orders     = 1000000
	OrderLines = 3000000
)

// SetupSQL returns the statements that build the demo at the given scale.
func SetupSQL(scale float64) []string {
	c := int(Customers * scale)
	o := int(Orders * scale)
	l := int(OrderLines * scale)
	if c < 1000 {
		c, o, l = 1000, 5000, 15000
	}
	return []string{
		"DROP SCHEMA IF EXISTS " + Schema + " CASCADE",
		"CREATE SCHEMA " + Schema,
		// Every second customer is a gold partner; the newest thirty
		// are platinum. tier and channel always go together.
		fmt.Sprintf(`CREATE TABLE %[1]s.customers AS
SELECT g AS id,
       jsonb_build_object(
         'tier',    CASE WHEN g %% 2 = 0 THEN 'gold' WHEN g > %[2]d THEN 'platinum' ELSE 'basic' END,
         'channel', CASE WHEN g %% 2 = 0 THEN 'partner' ELSE 'web' END) AS attrs,
       g %% 100 AS region,
       timestamp '2024-01-01' + g * interval '1 minute' AS created_at
FROM generate_series(1, %[3]d) g`, Schema, c-30, c),
		fmt.Sprintf(`CREATE TABLE %s.orders AS
SELECT g AS id, ((g::bigint * 7) %% %d)::int + 1 AS customer_id, (g * 13) %% 500 AS total
FROM generate_series(1, %d) g`, Schema, c, o),
		fmt.Sprintf(`CREATE TABLE %s.order_lines AS
SELECT g AS id, ((g::bigint * 11) %% %d)::int + 1 AS order_id, g %% 1000 AS product_id, (g %% 7) + 1 AS qty
FROM generate_series(1, %d) g`, Schema, o, l),
		"ALTER TABLE " + Schema + ".customers ADD PRIMARY KEY (id)",
		"CREATE INDEX ON " + Schema + ".customers (created_at)",
		"ALTER TABLE " + Schema + ".orders ADD PRIMARY KEY (id)",
		"CREATE INDEX ON " + Schema + ".orders (customer_id)",
		"ALTER TABLE " + Schema + ".order_lines ADD PRIMARY KEY (id)",
		"CREATE INDEX ON " + Schema + ".order_lines (order_id)",
		"VACUUM ANALYZE " + Schema + ".customers",
		"VACUUM ANALYZE " + Schema + ".orders",
		"VACUUM ANALYZE " + Schema + ".order_lines",
	}
}

// Workload is the demo's workload file. Each shape is there for a reason,
// given in the comment above it.
const Workload = `-- LQO demo workload, for the lqo_demo schema that "lqo demo" builds.

-- 1. A trap. Two JSON conditions the planner guesses match 0.0025% of
--    customers actually match half of them, so its nested loops run
--    hundreds of thousands of times. A hash join is faster on both texts.
SELECT sum(l.qty)
  FROM lqo_demo.customers c
  JOIN lqo_demo.orders o ON o.customer_id = c.id
  JOIN lqo_demo.order_lines l ON l.order_id = o.id
 WHERE c.attrs->>'tier' = 'gold' AND c.attrs->>'channel' = 'partner';

SELECT sum(l.qty)
  FROM lqo_demo.customers c
  JOIN lqo_demo.orders o ON o.customer_id = c.id
  JOIN lqo_demo.order_lines l ON l.order_id = o.id
 WHERE c.attrs->>'tier' = 'basic' AND c.attrs->>'channel' = 'web';

-- 2. A trap the other way. The planner expects 0.5% of customers to be
--    platinum and walks the created_at index, hoping to find ten soon.
--    The only thirty are the newest, so it reads the whole index.
SELECT id, created_at
  FROM lqo_demo.customers
 WHERE attrs->>'tier' = 'platinum'
 ORDER BY created_at
 LIMIT 10;

-- 3. Not a trap for every value. 'partner' turns up every second row and
--    the index finds twenty at once; 'reseller' never does. A hint that
--    helps one value hurts the other, so LQO should leave this alone.
SELECT id FROM lqo_demo.customers WHERE attrs->>'channel' = 'partner' ORDER BY created_at DESC LIMIT 20;
SELECT id FROM lqo_demo.customers WHERE attrs->>'channel' = 'reseller' ORDER BY created_at DESC LIMIT 20;

-- 4. A good plan already: one customer, found by its key.
SELECT c.id, count(*), sum(l.qty)
  FROM lqo_demo.customers c
  JOIN lqo_demo.orders o ON o.customer_id = c.id
  JOIN lqo_demo.order_lines l ON l.order_id = o.id
 WHERE c.id = 4242
 GROUP BY c.id;

-- 5. A plain aggregate.
SELECT region, count(*) FROM lqo_demo.customers GROUP BY region ORDER BY region;

-- 6. A write. LQO never runs these; it lists them as skipped.
UPDATE lqo_demo.customers SET region = region WHERE id = 1;
`

// Setup builds the demo schema. It refuses a standby, where it can't write.
func Setup(ctx context.Context, conn *pgwire.Conn, scale float64, progress func(string)) error {
	rec, err := conn.QueryValue(ctx, "SELECT pg_is_in_recovery()")
	if err != nil {
		return err
	}
	if rec == "t" {
		return fmt.Errorf("this server is a standby; build the demo on the primary and let it replicate, or on a test server")
	}
	for _, s := range SetupSQL(scale) {
		if progress != nil {
			progress(firstLine(s))
		}
		if _, err := conn.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", firstLine(s), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
