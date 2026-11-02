# lqo-demo.sql (sql) against pg_stat_statements on the --dsn server (16 entries, 5 reads, 4 not reads, 7 on system catalogs only)

| Shape | Query | From the file | From pg_stat_statements | Same |
|---|---|---|---|---|
| `24eb83c2` | `SELECT id FROM lqo_demo.customers WHERE attrs->>'channel' =…` | keep the default plan | keep the default plan | yes |
| `5fcb12e3` | `SELECT c.id, count(*), sum(l.qty) FROM lqo_demo.customers c…` | keep the default plan | keep the default plan | yes |
| `6c73a7a6` | `SELECT id, created_at FROM lqo_demo.customers WHERE attrs->…` | "no index scan", −26% | keep the default plan | no |
| `b690ee77` | `SELECT region, count(*) FROM lqo_demo.customers GROUP BY re…` | keep the default plan | keep the default plan | yes |
| `ef02bfce` | `SELECT sum(l.qty * p.price) FROM lqo_demo.customers c JOIN …` | "no nested loop", −58% | "no nested loop", −57% | yes |

Shapes in both runs: 5, with the same verdict and hint: 4.
Suggestions: 2 from the file, 1 from pg_stat_statements.
