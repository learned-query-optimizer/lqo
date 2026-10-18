// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package workload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The three log files in testdata were written by a real PostgreSQL 16.15
// with log_min_duration_statement = 0, auto_explain in text and JSON
// format, and stderr, csvlog and jsonlog all switched on.

func texts(st []Statement) []string {
	var out []string
	for _, s := range st {
		out = append(out, s.Text)
	}
	return out
}

func wantStatements(t *testing.T, st []Statement, csvOrJSON bool) {
	t.Helper()
	got := texts(st)
	want := []string{
		"SET log_min_duration_statement = 0",
		"SELECT count(*) FROM exp.customers WHERE region = 5",
		"SELECT id\n  FROM exp.customers\n WHERE attrs->>'tier' = 'gold' -- a comment\n ORDER BY created_at LIMIT 3",
		"SELECT '41'::int + 1 AS a, 'it''s'::text AS b",
		"LOAD 'auto_explain'",
		"SET auto_explain.log_min_duration = 0",
		"SET auto_explain.log_parameter_max_length = -1",
		"SELECT count(*) FROM exp.customers c\n  JOIN exp.orders o ON o.customer_id = c.id\n WHERE c.id < 10",
		"SELECT region FROM exp.customers WHERE id = '7'",
	}
	// "SET log_min_duration_statement = -1" isn't here: once it has run,
	// the server stops logging durations, so it never logs itself.
	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("missing statement %q\ngot: %q", w, got)
		}
	}
	for _, g := range got {
		if strings.Contains(g, "1/0") {
			t.Errorf("a failed statement was read: %q", g)
		}
		if strings.HasPrefix(g, "SELECT $1") {
			t.Errorf("parse or bind step was read as a statement, or parameters weren't filled in: %q", g)
		}
	}
	for _, s := range st {
		if strings.HasPrefix(s.Text, "SELECT count(*) FROM exp.customers WHERE region") {
			if s.LoggedMs <= 0 {
				t.Errorf("duration not read: %v", s.LoggedMs)
			}
			if csvOrJSON {
				if s.Database != "lqo_test" {
					t.Errorf("database not read: %q", s.Database)
				}
				if s.QueryID == "" {
					t.Errorf("query ID not read")
				}
			}
		}
	}
}

func TestReadStderrLog(t *testing.T) {
	st, format, err := ReadFile("testdata/pg16-stderr.log", FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatStderr {
		t.Errorf("detected %q", format)
	}
	wantStatements(t, st, false)
}

func TestReadCSVLog(t *testing.T) {
	st, format, err := ReadFile("testdata/pg16.csv", FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatCSV {
		t.Errorf("detected %q", format)
	}
	wantStatements(t, st, true)
}

func TestReadJSONLog(t *testing.T) {
	st, format, err := ReadFile("testdata/pg16.json", FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatJSON {
		t.Errorf("detected %q", format)
	}
	wantStatements(t, st, true)
}

func TestDetectWithoutExtension(t *testing.T) {
	dir := t.TempDir()
	for src, want := range map[string]string{
		"testdata/pg16-stderr.log": FormatStderr,
		"testdata/pg16.csv":        FormatCSV,
		"testdata/pg16.json":       FormatJSON,
	} {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "postgresql-log")
		os.WriteFile(p, data, 0o600)
		if got := Detect(p, data); got != want {
			t.Errorf("%s: detected %q, want %q", src, got, want)
		}
	}
	if got := Detect("x", []byte("SELECT 1;\nSELECT 2;")); got != FormatSQL {
		t.Errorf("plain SQL detected as %q", got)
	}
}

func TestReadSQL(t *testing.T) {
	st := ReadSQL("w.sql", "-- top\nSELECT 1;\n\nSELECT 'a;\nb';\nSELECT 3")
	if len(st) != 3 {
		t.Fatalf("got %d: %q", len(st), texts(st))
	}
	if st[0].Source != "w.sql:2" || st[1].Source != "w.sql:4" || st[2].Source != "w.sql:6" {
		t.Errorf("sources: %s %s %s", st[0].Source, st[1].Source, st[2].Source)
	}
	if st[0].LoggedMs != -1 {
		t.Error("SQL files have no durations")
	}
}

func TestAutoExplainQueryKeepsJoinLines(t *testing.T) {
	q, _ := autoExplainQuery(" plan:\nQuery Text: SELECT *\n  FROM a\n  JOIN b on b.id = a.id\nHash Join  (cost=1.00..2.00 rows=1 width=4)\n  ->  Seq Scan on a  (cost=0.00..1.00 rows=1 width=4)")
	if q != "SELECT *\n  FROM a\n  JOIN b on b.id = a.id" {
		t.Errorf("got %q", q)
	}
}

func TestGroup(t *testing.T) {
	st := []Statement{
		{Text: "SELECT * FROM t WHERE id = 1", LoggedMs: 5},
		{Text: "SELECT * FROM t WHERE id = 2", LoggedMs: 50},
		{Text: "SELECT * FROM t WHERE id = 1", LoggedMs: 7},
		{Text: "select *  from t where id = 3", LoggedMs: 1},
		{Text: "SELECT * FROM u", LoggedMs: 100},
		{Text: "UPDATE t SET x = 1", LoggedMs: 1000},
	}
	shapes := Group(st)
	if len(shapes) != 3 {
		t.Fatalf("got %d shapes", len(shapes))
	}
	if !strings.HasPrefix(shapes[0].Normalized, "update") || shapes[0].Skip == "" {
		t.Errorf("slowest shape first, and the UPDATE skipped: %+v", shapes[0])
	}
	if shapes[1].Normalized != "select * from u" {
		t.Errorf("order: %q", shapes[1].Normalized)
	}
	sh := shapes[2]
	if sh.Calls != 4 || sh.LoggedMs != 63 || len(sh.Samples) != 3 {
		t.Errorf("calls %d, ms %v, samples %d", sh.Calls, sh.LoggedMs, len(sh.Samples))
	}
	if sh.Samples[0].Text != "SELECT * FROM t WHERE id = 2" || sh.Samples[1].Calls != 2 {
		t.Errorf("samples should be slowest first: %+v", sh.Samples)
	}
}

func TestFilterDatabase(t *testing.T) {
	st := []Statement{{Text: "a", Database: "shop"}, {Text: "b", Database: "other"}, {Text: "c"}}
	kept, dropped := FilterDatabase(st, "shop")
	if len(kept) != 2 || dropped != 1 {
		t.Errorf("kept %d, dropped %d", len(kept), dropped)
	}
}
