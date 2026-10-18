# LQO advisor report

- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-0ubuntu0.24.04.1), database lqo_test (primary, allowed with --allow-primary)
- **Workload:** lqo-demo.sql (sql): 8 statements, 6 shapes
- **Settings:** standard hints (13), 3 timed runs after a warm-up, up to 3 samples per shape, a hint must save 20%
- **Estimate:** one pass of the tested statements takes 2,218 ms now, 1,453 ms with these hints (−34%)

## Faster with a hint (2 of 5 tested)

### `891f0819` 1,061 ms → 684 ms (−36%)

```sql
SELECT sum(l.qty)
  FROM lqo_demo.customers c
  JOIN lqo_demo.orders o ON o.customer_id = c.id
  JOIN lqo_demo.order_lines l ON l.order_id = o.id
 WHERE c.attrs->>'tier' = 'gold' AND c.attrs->>'channel' = 'partner'
```

- **Hint:** no nested loop, 2 calls
- **Plan:** Nested Loop ×2, Bitmap Heap Scan on orders, Index Scan on order_lines → Hash Join ×2, Seq Scan on order_lines, Seq Scan on orders

```sql
SET LOCAL enable_nestloop = off;
```

### `6c73a7a6` 35.1 ms → 24.7 ms (−30%)

```sql
SELECT id, created_at
  FROM lqo_demo.customers
 WHERE attrs->>'tier' = 'platinum'
 ORDER BY created_at
 LIMIT 10
```

- **Hint:** no index scan, 1 call
- **Plan:** Index Scan on customers → Seq Scan on customers

```sql
SET LOCAL enable_indexscan = off;
SET LOCAL enable_indexonlyscan = off;
```

## Keep the default plan (3)

| Shape | Query | Time | Why |
|---|---|---|---|
| `24eb83c2` | SELECT id FROM lqo_demo.customers WHERE attrs->>'channel' =… | 17.5 ms | no other plan was clearly faster |
| `5fcb12e3` | SELECT c.id, count(*), sum(l.qty) FROM lqo_demo.customers c… | 0.21 ms | no other plan was clearly faster |
| `b690ee77` | SELECT region, count(*) FROM lqo_demo.customers GROUP BY re… | 25.5 ms | no other plan was clearly faster |

## Not tested (1)

| Shape | Query | Why |
|---|---|---|
| `48529e11` | UPDATE lqo_demo.customers SET region = region WHERE id = 1 | skipped: only reads are tested, and this is UPDATE |

Times are planning plus execution, as EXPLAIN ANALYZE reports them, median of 3 runs per plan.
