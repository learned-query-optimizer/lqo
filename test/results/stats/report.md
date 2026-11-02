# LQO advisor report

- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-0ubuntu0.24.04.1), database lqotest (primary, allowed with --allow-primary)
- **Workload:** pg_stat_statements on the --dsn server (16 entries, 5 reads, 4 not reads, 7 on system catalogs only): 13 statements, 5 shapes
- **Settings:** standard hints (13), 3 timed runs after a warm-up, up to 3 samples per shape, a hint must save 20%; row-count hints off: pg_hint_plan isn't installed or can't be loaded
- **Estimate:** one pass of the tested statements takes 12,118 ms now, 5,199 ms with these hints (−57%)

## Faster with a hint (1 of 5 tested)

### `ef02bfce` 2,006 ms → 853 ms (−57%)

```sql
SELECT sum(l.qty * p.price)
  FROM lqo_demo.customers c
  JOIN lqo_demo.orders o ON o.customer_id = c.id
  JOIN lqo_demo.order_lines l ON l.order_id = o.id
  JOIN lqo_demo.products p ON p.id = l.product_id
 WHERE c.attrs->>'channel' = 'partner' AND c.attrs->>'tier' = 'gold'
```

- **Hint:** no nested loop, 6 calls
- **Plan:** Nested Loop ×3, Bitmap Heap Scan on orders, Index Scan on order_lines, Index Scan on products → Hash Join ×3, Seq Scan on order_lines, Seq Scan on orders, Seq Scan on products
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_nestloop = off;
```

## Keep the default plan (4)

| Shape | Query | Time | Why |
|---|---|---|---|
| `6c73a7a6` | SELECT id, created_at FROM lqo_demo.customers WHERE attrs->… | 0.12 ms | no other plan was clearly faster |
| `24eb83c2` | SELECT id FROM lqo_demo.customers WHERE attrs->>'channel' =… | 0.16 ms | no other plan was clearly faster |
| `b690ee77` | SELECT region, count(*) FROM lqo_demo.customers GROUP BY re… | 26.4 ms | no other plan was clearly faster |
| `5fcb12e3` | SELECT c.id, count(*), sum(l.qty) FROM lqo_demo.customers c… | 0.26 ms | no other plan was clearly faster |

Times are planning plus execution, as EXPLAIN ANALYZE reports them, median of 3 runs per plan.
