// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package sqltext

import (
	"strings"
	"testing"
)

func TestLexKeepsEveryByte(t *testing.T) {
	inputs := []string{
		"SELECT 'it''s', E'a\\'b', $$x;y$$, $tag$ $$ $tag$, \"Col\"\"x\" FROM t -- c;\n/* a /* nested */ b */ WHERE x = $1",
		"select 1.5e10, .5, 0x1F, 1_000, x::int, a<>b, a->>'k', U&'d\\0061t'",
		"unterminated 'string",
		"unterminated /* comment",
		"émoji 'ü' \"naïve\"",
	}
	for _, in := range inputs {
		var b strings.Builder
		for _, tok := range Lex(in) {
			b.WriteString(tok.Text)
		}
		if b.String() != in {
			t.Errorf("Lex lost bytes:\n in: %q\nout: %q", in, b.String())
		}
	}
}

func TestLexKinds(t *testing.T) {
	toks := Code("SELECT $1, 'a;b', $q$x$q$, \"Id\", 42, -- hi\n x FROM t")
	want := []Kind{Word, Param, Op, String, Op, String, Op, Ident, Op, Number, Op, Word, Word, Word}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens: %v", len(toks), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d %q: kind %d, want %d", i, toks[i].Text, toks[i].Kind, k)
		}
	}
}

func TestSplit(t *testing.T) {
	script := `
-- a comment ; with a semicolon
SELECT 1;
SELECT 'a;b' AS x;  SELECT $$;$$;
/* only a comment; */
;
SELECT "semi;colon" FROM t
`
	got := Split(script)
	want := []string{"-- a comment ; with a semicolon\nSELECT 1", "SELECT 'a;b' AS x", "SELECT $$;$$", `SELECT "semi;colon" FROM t`}
	if len(got) != len(want) {
		t.Fatalf("got %d statements: %q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct{ a, b string }{
		{"SELECT * FROM t WHERE id = 5", "select *   from t where id = 42 -- note"},
		{"SELECT * FROM t WHERE id = -5", "SELECT * FROM t WHERE id = 7"},
		{"SELECT * FROM t WHERE id IN (1, 2, 3)", "SELECT * FROM t WHERE id IN (4)"},
		{"SELECT * FROM t WHERE id = ANY(ARRAY[1,2])", "SELECT * FROM t WHERE id = ANY(ARRAY[9, 8, 7, 6])"},
		{"SELECT * FROM t WHERE name = 'x' AND d > '2024-01-01'", "SELECT * FROM t WHERE name = $1 AND d > $2"},
		{"SELECT a FROM t LIMIT 10", "SELECT a FROM t LIMIT 99"},
	}
	for _, c := range cases {
		if Normalize(c.a) != Normalize(c.b) {
			t.Errorf("should match:\n %s\n %s", Normalize(c.a), Normalize(c.b))
		}
		if Fingerprint(c.a) != Fingerprint(c.b) {
			t.Errorf("fingerprints differ for %q and %q", c.a, c.b)
		}
	}
	different := []struct{ a, b string }{
		{"SELECT a FROM t", "SELECT b FROM t"},
		{`SELECT "A" FROM t`, `SELECT "a" FROM t`},
		{"SELECT a - 1 FROM t", "SELECT a FROM t"},
		{"SELECT * FROM t WHERE x IN (SELECT y FROM u)", "SELECT * FROM t WHERE x IN (1)"},
	}
	for _, c := range different {
		if Fingerprint(c.a) == Fingerprint(c.b) {
			t.Errorf("should differ: %q and %q", c.a, c.b)
		}
	}
	if got := Normalize("SELECT a - 1 FROM t"); got != "select a - ? from t" {
		t.Errorf("subtraction lost: %q", got)
	}
	if len(Fingerprint("SELECT 1")) != 16 {
		t.Error("fingerprint should be 16 hex digits")
	}
}

func TestOneLine(t *testing.T) {
	got := OneLine("SELECT a,\n   b -- comment\nFROM t", 0)
	if got != "SELECT a, b FROM t" {
		t.Errorf("got %q", got)
	}
	if got := OneLine("SELECT abcdefghij", 10); got != "SELECT ab…" {
		t.Errorf("got %q", got)
	}
}

func TestSubstitute(t *testing.T) {
	got := Substitute("SELECT * FROM t WHERE a = $1 AND b = $2 AND c = '$1' AND d = $3", map[int]string{1: "'x'", 2: "NULL"})
	want := "SELECT * FROM t WHERE a = 'x' AND b = NULL AND c = '$1' AND d = $3"
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestClassify(t *testing.T) {
	ok := []string{
		"SELECT 1",
		"  with x as (select 1) select * from x",
		"(SELECT a FROM t) UNION (SELECT b FROM u)",
		"VALUES (1), (2)",
		"TABLE t",
		"SELECT lower(name), log(2, 8) FROM t",
		"SELECT * FROM t WHERE name = 'insert into x'",
		`SELECT "update" FROM t`,
	}
	for _, s := range ok {
		if r := Classify(s); r != "" {
			t.Errorf("%q should be allowed, got %q", s, r)
		}
	}
	skipped := map[string]string{
		"INSERT INTO t VALUES (1)":                                     "only reads",
		"UPDATE t SET a = 1":                                           "only reads",
		"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d":        "DELETE",
		"SELECT * INTO new_t FROM t":                                   "SELECT INTO",
		"SELECT * FROM t FOR UPDATE":                                   "locks rows",
		"SELECT * FROM t FOR NO KEY UPDATE":                            "locks rows",
		"SELECT * FROM t WHERE id = $1":                                "parameter",
		"SELECT pg_sleep(1)":                                           "pg_sleep",
		"SELECT pg_catalog.pg_terminate_backend(42)":                   "pg_terminate_backend",
		"SELECT set_config('enable_nestloop', 'off', false)":           "set_config",
		"SELECT nextval('s')":                                          "nextval",
		"SELECT * FROM dblink('host=x', 'select 1') AS t(a int)":       "dblink",
		"SELECT lo_import('/etc/passwd')":                              "lo_import",
		"SELECT pg_advisory_lock(1)":                                   "pg_advisory_lock",
		"EXPLAIN SELECT 1":                                             "EXPLAIN",
		"SELECT 1; SELECT 2":                                           "more than one",
		"-- nothing":                                                   "empty",
		"MERGE INTO t USING u ON t.id = u.id WHEN MATCHED THEN DELETE": "only reads",
		`SELECT "pg_advisory_lock"(424242)`:                            "pg_advisory_lock",
		`SELECT pg_catalog."pg_terminate_backend"(1)`:                  "pg_terminate_backend",
		`SELECT "PG_SLEEP"(1)`:                                         "pg_sleep",
		`SELECT U&"pg\0073leep"(1)`:                                    "Unicode",
		"SELECT query_to_xml('delete from t', true, false, '')":        "query_to_xml",
		"SELECT pg_replication_slot_advance('s', '0/0')":               "pg_replication_slot_advance",
		"SELECT 1 AS a --note\r; COMMIT; CREATE TABLE t(x int)":        "more than one",
	}
	for s, want := range skipped {
		r := Classify(s)
		if r == "" || !strings.Contains(r, want) {
			t.Errorf("%q: got %q, want a reason with %q", s, r, want)
		}
	}
}

func TestTrimLeadingComments(t *testing.T) {
	got := TrimLeadingComments("-- a note\n/* another */\n  SELECT 1 -- trailing stays")
	if got != "SELECT 1 -- trailing stays" {
		t.Errorf("got %q", got)
	}
	if TrimLeadingComments("-- only") != "" {
		t.Error("a comment alone should trim to nothing")
	}
}
