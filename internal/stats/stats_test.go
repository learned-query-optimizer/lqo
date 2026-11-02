// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package stats

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/workload"
)

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.7", "1.8", true}, {"1.10", "1.9", false}, {"1.9", "1.9", false}, {"1.11", "1.8", false}} {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestSystemOnly(t *testing.T) {
	for q, want := range map[string]bool{
		"SELECT version()": true,
		"SELECT name FROM pg_settings WHERE name IN ($1)":                              true,
		"SELECT * FROM pg_catalog.pg_class c JOIN information_schema.tables t ON true": true,
		"SELECT calls FROM pg_stat_statements":                                         true,
		"SELECT * FROM app.orders o JOIN pg_catalog.pg_class c ON c.oid = o.id":        false,
		"SELECT * FROM orders": false,
		"WITH x AS (SELECT * FROM shop.customers) SELECT * FROM x": false,
	} {
		if got := systemOnly(q); got != want {
			t.Errorf("systemOnly(%q) = %v", q, got)
		}
	}
}

func TestAlternate(t *testing.T) {
	in := [][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}}
	var got []string
	for _, x := range alternate(in) {
		got = append(got, x[0])
	}
	if strings.Join(got, "") != "aebdc" {
		t.Errorf("got %v", got)
	}
}

func TestArrayLiteral(t *testing.T) {
	if got := arrayLiteral([]string{`a"b`, `it's`, `c\d`}); got != `'{"a\"b","it''s","c\\d"}'` {
		t.Errorf("got %s", got)
	}
}

func connect(t *testing.T) *pgwire.Conn {
	t.Helper()
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	cfg, err := pgwire.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	c, err := pgwire.Connect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func setup(t *testing.T, c *pgwire.Conn) {
	t.Helper()
	ctx := context.Background()
	for _, s := range []string{
		"DROP SCHEMA IF EXISTS lqo_stats_test CASCADE",
		"CREATE SCHEMA lqo_stats_test",
		// tier and channel go together: gold customers come through partners.
		`CREATE TABLE lqo_stats_test.customers (id int PRIMARY KEY, attrs jsonb, created date)`,
		`INSERT INTO lqo_stats_test.customers
		 SELECT g, jsonb_build_object('tier', CASE WHEN g % 10 = 0 THEN 'gold' ELSE 'basic' END,
		                              'channel', CASE WHEN g % 10 = 0 THEN 'partner' ELSE 'web' END),
		        date '2026-01-01' + (g % 300)
		 FROM generate_series(1, 20000) g`,
		`CREATE TABLE lqo_stats_test.orders (id int PRIMARY KEY, customer_id int, total numeric)`,
		`INSERT INTO lqo_stats_test.orders SELECT g, 1 + g % 20000, g % 500 FROM generate_series(1, 50000) g`,
		`CREATE VIEW lqo_stats_test.big_orders AS SELECT * FROM lqo_stats_test.orders WHERE total > 400`,
		"ANALYZE lqo_stats_test.customers",
		"ANALYZE lqo_stats_test.orders",
	} {
		if _, err := c.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	t.Cleanup(func() { c.Exec(context.Background(), "DROP SCHEMA IF EXISTS lqo_stats_test CASCADE") })
}

func TestFill(t *testing.T) {
	c := connect(t)
	setup(t, c)
	ctx := context.Background()
	f := &Filler{Conn: c, Samples: 3, Timeout: 5 * time.Second}

	// Parameters on one table come from the same rows, so a gold tier goes
	// with the partner channel, never with web.
	q := "SELECT count(*) FROM lqo_stats_test.customers c JOIN lqo_stats_test.orders o ON o.customer_id = c.id WHERE c.attrs->>'tier' = $1 AND c.attrs->>'channel' = $2 AND o.total > $3 LIMIT $4"
	texts, why, err := f.Fill(ctx, q)
	if err != nil || why != "" {
		t.Fatalf("Fill: %v %s", err, why)
	}
	if len(texts) != 2 {
		t.Fatalf("want the two combinations that exist, got %d: %q", len(texts), texts)
	}
	for _, tx := range texts {
		gold := strings.Contains(tx, "'gold'")
		partner := strings.Contains(tx, "'partner'")
		if gold != partner {
			t.Errorf("values that don't go together: %s", tx)
		}
		if !strings.Contains(tx, "LIMIT 100") || strings.Contains(tx, "$") {
			t.Errorf("not filled: %s", tx)
		}
		if sqltext.Fingerprint(tx) != sqltext.Fingerprint(q) {
			t.Errorf("filled text has another shape: %s", tx)
		}
		if _, err := c.Exec(ctx, tx); err != nil {
			t.Errorf("filled text doesn't run: %v\n%s", err, tx)
		}
	}
	// The most frequent combination comes first.
	if !strings.Contains(texts[0], "'basic'") {
		t.Errorf("first text isn't the most frequent combination: %s", texts[0])
	}

	// pg_stat_statements writes JSON keys as parameters too. Keys come
	// from the rows; two keys on one column differ, and the values compared
	// with them come from the same rows.
	q = "SELECT id FROM lqo_stats_test.customers c WHERE c.attrs->>$1 = $2 AND c.attrs->>$3 = $4 ORDER BY id LIMIT $5"
	texts, why, err = f.Fill(ctx, q)
	if err != nil || why != "" || len(texts) == 0 {
		t.Fatalf("JSON keys: %v %s", err, why)
	}
	for _, tx := range texts {
		if strings.Contains(tx, "$") || strings.Count(tx, "'tier'")+strings.Count(tx, "'channel'") != 2 || strings.Count(tx, "'tier'") != 1 {
			t.Errorf("keys: %s", tx)
		}
		if strings.Contains(tx, "'gold'") != strings.Contains(tx, "'partner'") {
			t.Errorf("values that don't go together: %s", tx)
		}
		if _, err := c.Exec(ctx, tx); err != nil {
			t.Errorf("doesn't run: %v\n%s", err, tx)
		}
	}

	// Unqualified columns, BETWEEN, IN lists, arrays, a view, casts.
	for _, q := range []string{
		"SELECT id FROM lqo_stats_test.customers WHERE created BETWEEN $1 AND $2 ORDER BY id OFFSET $3",
		"SELECT * FROM lqo_stats_test.orders WHERE customer_id IN ($1, $2, $3)",
		"SELECT * FROM lqo_stats_test.orders WHERE customer_id = ANY($1::int[])",
		"SELECT * FROM lqo_stats_test.big_orders b WHERE b.total = $1::numeric",
		"SELECT * FROM lqo_stats_test.orders o, lqo_stats_test.customers c WHERE o.customer_id = c.id AND created < $1",
	} {
		texts, why, err := f.Fill(ctx, q)
		if err != nil || why != "" || len(texts) == 0 {
			t.Errorf("%s: %v %s", q, err, why)
			continue
		}
		for _, tx := range texts {
			if _, err := c.Exec(ctx, tx); err != nil {
				t.Errorf("filled text doesn't run: %v\n%s", err, tx)
			}
		}
	}
	// IN list parameters take different values.
	texts, _, _ = f.Fill(ctx, "SELECT * FROM lqo_stats_test.orders WHERE customer_id IN ($1, $2)")
	if len(texts) == 0 {
		t.Fatal("no texts")
	}
	in := texts[0][strings.Index(texts[0], "IN"):]
	parts := strings.Split(strings.Trim(in[2:], " ()"), ",")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == strings.TrimSpace(parts[1]) {
		t.Errorf("IN list with one value twice: %s", texts[0])
	}

	// What LQO can't fill, it says why, and nothing runs.
	for q, want := range map[string]string{
		"SELECT * FROM lqo_stats_test.orders WHERE total > now()::date - $1::date": "isn't compared with a column",
		"SELECT * FROM lqo_stats_test.orders WHERE nosuch = $1":                    "no table here has nosuch",
		"SELECT * FROM lqo_stats_test.orders o WHERE x.total = $1":                 "can't tell which table x is",
	} {
		texts, why, err := f.Fill(ctx, q)
		if err != nil || len(texts) != 0 || !strings.Contains(why, want) {
			t.Errorf("%s: texts %q, why %q, err %v; want %q", q, texts, why, err, want)
		}
	}
	if c.TxStatus() != 'I' {
		t.Errorf("left a transaction open: %c", c.TxStatus())
	}
}

// skip skips, unless CI says the extension must be there.
func skip(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("LQO_REQUIRE_STATS") != "" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func TestReadAndGroup(t *testing.T) {
	c := connect(t)
	ctx := context.Background()
	v, err := c.QueryValue(ctx, "SELECT count(*) FROM pg_extension WHERE extname = 'pg_stat_statements'")
	if err != nil {
		t.Fatal(err)
	}
	if v == "0" {
		if _, err := c.Exec(ctx, "CREATE EXTENSION pg_stat_statements"); err != nil {
			skip(t, "pg_stat_statements isn't available: %v", err)
		}
	}
	if _, err := c.Exec(ctx, "SELECT pg_stat_statements_reset()"); err != nil {
		skip(t, "pg_stat_statements isn't loaded: %v", err)
	}
	setup(t, c)
	if _, err := c.Exec(ctx, "SELECT pg_stat_statements_reset()"); err != nil {
		t.Fatal(err)
	}
	q := "SELECT count(*) FROM lqo_stats_test.customers c WHERE c.attrs->>'tier' = 'gold' AND c.created > date '2026-03-01'"
	for i := 0; i < 5; i++ {
		if _, err := c.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	c.Exec(ctx, "UPDATE lqo_stats_test.orders SET total = total WHERE id = 1")
	c.Exec(ctx, "SELECT x FROM lqo_stats_test.customers WHERE id = 1 AND x > now() - '1 day'::interval")

	entries, sum, err := Read(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	var e *Entry
	for i := range entries {
		if strings.Contains(entries[i].Query, "lqo_stats_test.customers c") {
			e = &entries[i]
		}
		if strings.Contains(strings.ToUpper(entries[i].Query), "UPDATE") {
			t.Errorf("kept a write: %s", entries[i].Query)
		}
	}
	if e == nil {
		t.Fatalf("query not read; %s; %+v", sum, entries)
	}
	if e.Calls != 5 || !strings.Contains(e.Query, "$1") {
		t.Errorf("entry: %+v", e)
	}
	if sum.NotReads == 0 {
		t.Errorf("summary counts no writes: %s", sum)
	}

	f := &Filler{Conn: c, Samples: 3}
	st, err := f.Statements(ctx, []Entry{*e})
	if err != nil {
		t.Fatal(err)
	}
	// A log line of the same shape: the view's totals win, and its text
	// comes first.
	logged := workload.Statement{Text: strings.Replace(q, "'gold'", "'basic'", 1), Source: "log:1", LoggedMs: 1e6}
	shapes := workload.Group(append(st, logged))
	if len(shapes) != 1 {
		t.Fatalf("%d shapes", len(shapes))
	}
	sh := shapes[0]
	if !sh.Stats || sh.Calls != 5 || sh.LoggedMs != e.TotalMs || !sh.Filled || sh.Skip != "" {
		t.Errorf("shape: stats %v calls %d ms %v (want %v) filled %v skip %q", sh.Stats, sh.Calls, sh.LoggedMs, e.TotalMs, sh.Filled, sh.Skip)
	}
	if sh.Samples[0].Source != "log:1" || len(sh.Samples) < 2 {
		t.Errorf("samples: %+v", sh.Samples)
	}
}
