// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package pgwire

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests that need a server read its address from LQO_TEST_DSN, and skip
// when it isn't set. LQO_TEST_MD5_DSN and LQO_TEST_TLS_DSN, when set, add
// tests for md5 passwords and for verified TLS.

func testConn(t *testing.T) *Conn {
	t.Helper()
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	return connect(t, dsn)
}

func connect(t *testing.T, dsn string) *Conn {
	t.Helper()
	cfg, err := ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Connect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func clearPGEnv(t *testing.T) {
	for _, k := range []string{"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSSLMODE", "PGSSLROOTCERT", "PGSSLCERT", "PGSSLKEY", "PGAPPNAME", "PGCONNECT_TIMEOUT", "PGOPTIONS", "PGPASSFILE"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", t.TempDir())
}

func TestParseURL(t *testing.T) {
	clearPGEnv(t)
	c, err := ParseConfig("postgres://ann:s%40cret@db.example.com:6543/shop?sslmode=verify-full&application_name=x&connect_timeout=5")
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "db.example.com" || c.Port != 6543 || c.User != "ann" || c.Password != "s@cret" || c.Database != "shop" ||
		c.SSLMode != "verify-full" || c.ApplicationName != "x" || c.ConnectTimeout != 5*time.Second {
		t.Errorf("%+v", c)
	}
	if _, err := ParseConfig("postgres://a@h1,h2/db"); err == nil {
		t.Error("several hosts should be refused")
	}
	if _, err := ParseConfig("postgres://a@h/db?bogus=1"); err == nil {
		t.Error("unknown setting accepted")
	}
	c, _ = ParseConfig("postgresql://u@[::1]:5433/d")
	if net, addr := c.Address(); net != "tcp" || addr != "[::1]:5433" {
		t.Errorf("IPv6 address: %s %s", net, addr)
	}
}

func TestParseKeywords(t *testing.T) {
	clearPGEnv(t)
	c, err := ParseConfig(`host=/var/run/postgresql port=5433 user=bob password='it\'s a secret' dbname = shop sslmode=disable`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "/var/run/postgresql" || c.Port != 5433 || c.User != "bob" || c.Password != "it's a secret" || c.Database != "shop" {
		t.Errorf("%+v", c)
	}
	if net, addr := c.Address(); net != "unix" || addr != "/var/run/postgresql/.s.PGSQL.5433" {
		t.Errorf("socket: %s %s", net, addr)
	}
	for _, bad := range []string{"host", "port=x", "sslmode=sometimes", "password='open", "nonsense=1", "connect_timeout=-1"} {
		if _, err := ParseConfig(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseDefaultsAndEnv(t *testing.T) {
	clearPGEnv(t)
	t.Setenv("PGHOST", "envhost")
	t.Setenv("PGUSER", "envuser")
	t.Setenv("PGPORT", "7777")
	c, err := ParseConfig("dbname=x")
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "envhost" || c.User != "envuser" || c.Port != 7777 || c.Database != "x" || c.SSLMode != "prefer" {
		t.Errorf("%+v", c)
	}
	c, _ = ParseConfig("host=h user=u")
	if c.Database != "u" {
		t.Errorf("database should default to the user: %q", c.Database)
	}
	if strings.Contains(c.Describe(), "secret") {
		t.Error("Describe shows the password")
	}
}

func TestPassFile(t *testing.T) {
	clearPGEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "pgpass")
	os.WriteFile(p, []byte("# comment\nother:5432:*:*:nope\ndb1:5432:shop:ann:pa\\:ss\n*:*:*:*:fallback\n"), 0o600)
	c, err := ParseConfig("host=db1 user=ann dbname=shop passfile=" + p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Password != "pa:ss" {
		t.Errorf("got %q", c.Password)
	}
	c, _ = ParseConfig("host=db2 user=x passfile=" + p)
	if c.Password != "fallback" {
		t.Errorf("wildcard: got %q", c.Password)
	}
	os.Chmod(p, 0o644)
	c, _ = ParseConfig("host=db1 user=ann dbname=shop passfile=" + p)
	if c.Password != "" {
		t.Error("a passfile others can read must be ignored")
	}
}

func TestExecAndErrors(t *testing.T) {
	c := testConn(t)
	ctx := context.Background()
	res, err := c.Exec(ctx, "SELECT 1 AS a, NULL::text AS b, 'x''y' AS c; SELECT 2")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || len(res[0].Columns) != 3 || res[0].Columns[2] != "c" {
		t.Fatalf("results: %+v", res)
	}
	row := res[0].Rows[0]
	if *row[0] != "1" || row[1] != nil || *row[2] != "x'y" || res[0].Tag != "SELECT 1" {
		t.Errorf("row: %v %v %v tag %q", *row[0], row[1], *row[2], res[0].Tag)
	}
	_, err = c.Exec(ctx, "SELECT 1/0")
	var pe *PgError
	if !errors.As(err, &pe) || pe.Code != "22012" || !IsCode(err, "22012") {
		t.Fatalf("want division by zero, got %v", err)
	}
	if v, err := c.QueryValue(ctx, "SELECT 'still fine'"); err != nil || v != "still fine" {
		t.Errorf("connection not usable after an error: %v %q", err, v)
	}
	if _, err := c.Exec(ctx, ""); err != nil {
		t.Errorf("empty query: %v", err)
	}
	if c.ServerVersionNum() < 100000 {
		t.Errorf("server version %d", c.ServerVersionNum())
	}
	if c.TxStatus() != 'I' {
		t.Errorf("tx status %c", c.TxStatus())
	}
	c.Exec(ctx, "BEGIN")
	if c.TxStatus() != 'T' {
		t.Errorf("in a transaction: %c", c.TxStatus())
	}
	c.Exec(ctx, "SELECT 1/0")
	if c.TxStatus() != 'E' {
		t.Errorf("failed transaction: %c", c.TxStatus())
	}
	c.Exec(ctx, "ROLLBACK")
}

func TestCopyIsRefused(t *testing.T) {
	c := testConn(t)
	ctx := context.Background()
	c.Exec(ctx, "CREATE TEMP TABLE copy_t (a int)")
	if _, err := c.Exec(ctx, "COPY copy_t FROM STDIN"); err == nil {
		t.Error("COPY FROM STDIN should fail")
	}
	if _, err := c.Exec(ctx, "COPY (SELECT 1) TO STDOUT"); err != nil {
		t.Errorf("COPY TO STDOUT: %v", err)
	}
	if v, err := c.QueryValue(ctx, "SELECT 'ok'"); err != nil || v != "ok" {
		t.Errorf("not usable after COPY: %v", err)
	}
}

func TestStatementTimeout(t *testing.T) {
	c := testConn(t)
	ctx := context.Background()
	c.Exec(ctx, "SET statement_timeout = 100")
	start := time.Now()
	_, err := c.Exec(ctx, "SELECT pg_sleep(5)")
	if !IsCode(err, CodeQueryCanceled) {
		t.Fatalf("want a cancelled statement, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("statement_timeout took too long")
	}
	c.Exec(ctx, "RESET statement_timeout")
}

func TestContextCancelsTheQuery(t *testing.T) {
	c := testConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Exec(ctx, "SELECT pg_sleep(30)")
	if err == nil {
		t.Fatal("the query should have been cancelled")
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Errorf("cancelling took %s", d)
	}
	if !c.Broken() {
		if v, err := c.QueryValue(context.Background(), "SELECT 'after cancel'"); err != nil || v != "after cancel" {
			t.Errorf("connection not usable after a cancel: %v", err)
		}
	}
}

func TestBadPassword(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" || !strings.Contains(dsn, "@") || strings.Contains(dsn, "/var/run") {
		t.Skip("needs a TCP LQO_TEST_DSN with a password")
	}
	cfg, err := ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password == "" {
		t.Skip("LQO_TEST_DSN has no password")
	}
	cfg.Password = "definitely wrong"
	_, err = Connect(context.Background(), cfg)
	if !IsCode(err, "28P01") {
		t.Errorf("want an authentication failure, got %v", err)
	}
}

func TestMD5Login(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_MD5_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_MD5_DSN not set")
	}
	c := connect(t, dsn)
	if _, err := c.QueryValue(context.Background(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifiedTLS(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_TLS_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_TLS_DSN not set")
	}
	c := connect(t, dsn)
	if !c.UsedTLS() {
		t.Error("expected TLS")
	}
	cfg, _ := ParseConfig(dsn)
	cfg.Host = "127.0.0.1" // the certificate names localhost, not the IP
	if _, err := Connect(context.Background(), cfg); err == nil {
		t.Error("verify-full should reject a host name the certificate doesn't name")
	}
}

func TestConnectRefused(t *testing.T) {
	clearPGEnv(t)
	cfg, _ := ParseConfig("host=127.0.0.1 port=1 user=x sslmode=disable connect_timeout=2")
	if _, err := Connect(context.Background(), cfg); err == nil {
		t.Error("connecting to port 1 should fail")
	}
}
