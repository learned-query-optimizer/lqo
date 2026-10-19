# LQO advisor report

- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-0ubuntu0.24.04.1), database lqo_test (primary, allowed with --allow-primary)
- **Workload:** lqo-demo.sql (sql): 8 statements, 6 shapes
- **Settings:** standard hints (13), 3 timed runs after a warm-up, up to 3 samples per shape, a hint must save 20%
- **Estimate:** one pass of the tested statements takes 4,715 ms now, 2,259 ms with these hints (−52%)

## Faster with a hint (2 of 5 tested)

### `ef02bfce` 2,297 ms → 1,078 ms (−53%)

```sql
SELECT sum(l.qty * p.price)
  FROM lqo_demo.customers c
  JOIN lqo_demo.orders o ON o.customer_id = c.id
  JOIN lqo_demo.order_lines l ON l.order_id = o.id
  JOIN lqo_demo.products p ON p.id = l.product_id
 WHERE c.attrs->>'tier' = 'gold' AND c.attrs->>'channel' = 'partner'
```

- **Hint:** no nested loop, 2 calls
- **Plan:** Nested Loop ×3, Bitmap Heap Scan on orders, Index Scan on order_lines, Index Scan on products → Hash Join ×3, Seq Scan on order_lines, Seq Scan on orders, Seq Scan on products

```sql
SET LOCAL enable_nestloop = off;
```

### `6c73a7a6` 48.4 ms → 30.9 ms (−36%)

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
| `24eb83c2` | SELECT id FROM lqo_demo.customers WHERE attrs->>'channel' =… | 20.7 ms | a hint helped one sample but was slower on another |
| `5fcb12e3` | SELECT c.id, count(*), sum(l.qty) FROM lqo_demo.customers c… | 0.27 ms | no other plan was clearly faster |
| `b690ee77` | SELECT region, count(*) FROM lqo_demo.customers GROUP BY re… | 30.9 ms | no other plan was clearly faster |

## Not tested (1)

| Shape | Query | Why |
|---|---|---|
| `48529e11` | UPDATE lqo_demo.customers SET region = region WHERE id = 1 | skipped: only reads are tested, and this is UPDATE |

Times are planning plus execution, as EXPLAIN ANALYZE reports them, median of 3 runs per plan.
