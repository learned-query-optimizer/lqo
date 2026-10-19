// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
)

// Tests that need a server read LQO_TEST_DSN (a password login over TCP) and
// skip without it. LQO_TEST_MD5_DSN adds an md5 login. Tests that use psql or
// pgbench skip when those aren't on the PATH.

type testProxy struct {
	srv  *Server
	addr *net.TCPAddr
	cert string // PEM file of the proxy's certificate, when it has TLS
}

func upstreamDSN(t *testing.T, env string) string {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip(env + " not set")
	}
	return dsn
}

// startProxy runs a proxy in front of the server in dsn. sslmode in up
// decides the proxy's own connection to the server.
func startProxy(t *testing.T, dsn, upSSL string, withTLS bool, w Watcher) *testProxy {
	t.Helper()
	cfg, err := pgwire.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SSLMode = upSSL
	cfg.SSLRootCert = ""
	tp := &testProxy{}
	pc := Config{Upstream: cfg, Watch: w, StartupTimeout: 10 * time.Second}
	if withTLS {
		pc.TLS, tp.cert = selfSigned(t)
	}
	ln, err := Listen("127.0.0.1:0", 0)
	if err != nil {
		t.Fatal(err)
	}
	tp.addr = ln.Addr().(*net.TCPAddr)
	tp.srv = New(pc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tp.srv.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
		tp.srv.Shutdown(2 * time.Second)
	})
	return tp
}

// via is dsn pointed at the proxy, with the given sslmode.
func (tp *testProxy) via(t *testing.T, dsn, sslmode string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(tp.addr.Port))
	q := u.Query()
	q.Set("sslmode", sslmode)
	q.Del("sslrootcert")
	u.RawQuery = q.Encode()
	return u.String()
}

func connect(t *testing.T, dsn string) *pgwire.Conn {
	t.Helper()
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

func selfSigned(t *testing.T) (*tls.Config, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lqo-test"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "proxy.crt")
	os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}, path
}

var relayQueries = []string{
	"SELECT 1 AS a, NULL::text AS b, 'x''y' AS c, now()::date - now()::date AS d",
	"SELECT g, md5(g::text), repeat('é', g % 50) FROM generate_series(1, 20000) g",
	"SELECT 1; SELECT 2, 3",
	"CREATE TEMP TABLE lqo_proxy_t(id int, v text)",
	"INSERT INTO lqo_proxy_t SELECT g, 'v' || g FROM generate_series(1, 100) g",
	"UPDATE lqo_proxy_t SET v = v || '!' WHERE id % 2 = 0",
	"SELECT count(*), sum(id), max(v) FROM lqo_proxy_t",
	"SELECT 1/0",
	"SELECT * FROM lqo_no_such_table",
	"BEGIN",
	"SELECT 1/0",
	"SELECT 1",
	"ROLLBACK",
	"SELECT repeat('x', 3000000) AS big",
	"DO $$ BEGIN RAISE NOTICE 'hello'; END $$",
}

type outcome struct {
	Results []pgwire.Result
	Code    string
	Message string
}

func runAll(t *testing.T, c *pgwire.Conn, extended bool) []outcome {
	var out []outcome
	for _, q := range relayQueries {
		var res []pgwire.Result
		var err error
		if extended && !strings.Contains(q, ";") {
			res, err = c.ExecOne(context.Background(), q)
		} else {
			res, err = c.Exec(context.Background(), q)
		}
		o := outcome{Results: res}
		var pe *pgwire.PgError
		if errors.As(err, &pe) {
			o.Code, o.Message = pe.Code, pe.Message
		} else if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		out = append(out, o)
	}
	return out
}

func TestRelayGivesTheSameResults(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	tp := startProxy(t, dsn, "disable", false, nil)
	for _, extended := range []bool{false, true} {
		direct := runAll(t, connect(t, dsn), extended)
		proxied := runAll(t, connect(t, tp.via(t, dsn, "disable")), extended)
		if !reflect.DeepEqual(direct, proxied) {
			for i := range direct {
				if !reflect.DeepEqual(direct[i], proxied[i]) {
					t.Errorf("extended=%v, %q: results differ through the proxy", extended, relayQueries[i])
				}
			}
		}
	}
}

// serverTLS skips a test that needs the server itself to offer TLS.
func serverTLS(t *testing.T, dsn string) {
	t.Helper()
	c := connect(t, dsn)
	if v, err := c.QueryValue(context.Background(), "SHOW ssl"); err != nil || v != "on" {
		t.Skip("the test server doesn't offer TLS")
	}
}

func TestTLSOnBothSides(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	serverTLS(t, dsn)
	tp := startProxy(t, dsn, "require", true, nil)
	// Verified against the proxy's own certificate.
	c := connect(t, tp.via(t, dsn, "verify-full")+"&sslrootcert="+url.QueryEscape(tp.cert))
	if !c.UsedTLS() {
		t.Fatal("no TLS between client and proxy")
	}
	v, err := c.QueryValue(context.Background(), "SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()")
	if err != nil {
		t.Fatal(err)
	}
	if v != "t" {
		t.Errorf("the proxy's connection to the server isn't TLS: %q", v)
	}
}

func TestNoTLSOffered(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	tp := startProxy(t, dsn, "disable", false, nil)
	cfg, _ := pgwire.ParseConfig(tp.via(t, dsn, "require"))
	if _, err := pgwire.Connect(context.Background(), cfg); err == nil {
		t.Error("sslmode=require succeeded against a proxy without a certificate")
	}
	c := connect(t, tp.via(t, dsn, "prefer"))
	if c.UsedTLS() {
		t.Error("TLS used without a certificate")
	}
}

func TestMD5Login(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_MD5_DSN")
	tp := startProxy(t, dsn, "require", true, nil)
	c := connect(t, tp.via(t, dsn, "require"))
	if v, err := c.QueryValue(context.Background(), "SELECT current_user"); err != nil || v != "md5user" {
		t.Errorf("%v %q", err, v)
	}
}

func TestWrongPasswordReachesTheClient(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	tp := startProxy(t, dsn, "disable", false, nil)
	u, _ := url.Parse(tp.via(t, dsn, "disable"))
	u.User = url.UserPassword(u.User.Username(), "wrong")
	cfg, _ := pgwire.ParseConfig(u.String())
	_, err := pgwire.Connect(context.Background(), cfg)
	if !pgwire.IsCode(err, "28P01") {
		t.Errorf("want invalid_password from the server, got %v", err)
	}
}

func TestCancelGoesThroughTheProxy(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	tp := startProxy(t, dsn, "disable", false, nil)
	c := connect(t, tp.via(t, dsn, "disable"))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Exec(ctx, "SELECT pg_sleep(20)")
	if err == nil {
		t.Fatal("pg_sleep wasn't stopped")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("cancel took %v", d)
	}
	if v, err := c.QueryValue(context.Background(), "SELECT 'still fine'"); err != nil || v != "still fine" {
		t.Errorf("connection not usable after the cancel: %v %q", err, v)
	}
}

func TestUpstreamDownGivesAClearError(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	cfg, _ := pgwire.ParseConfig(dsn)
	cfg.Port = 1
	cfg.SSLMode = "disable"
	ln, err := Listen("127.0.0.1:0", 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{Upstream: cfg})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx, ln)
	tp := &testProxy{addr: ln.Addr().(*net.TCPAddr)}
	ccfg, _ := pgwire.ParseConfig(tp.via(t, dsn, "disable"))
	_, err = pgwire.Connect(context.Background(), ccfg)
	if err == nil || !strings.Contains(err.Error(), "couldn't reach the database server") {
		t.Errorf("got %v", err)
	}
}

func TestUnixSocket(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	cfg, _ := pgwire.ParseConfig(dsn)
	cfg.SSLMode = "disable"
	dir, err := os.MkdirTemp("", "lqo")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ln, err := Listen(dir, 6433)
	if err != nil {
		t.Skip("no Unix sockets here: ", err)
	}
	srv := New(Config{Upstream: cfg})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx, ln)
	ccfg, _ := pgwire.ParseConfig(dsn)
	ccfg.Host, ccfg.Port, ccfg.SSLMode = dir, 6433, "disable"
	c, err := pgwire.Connect(context.Background(), ccfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if v, err := c.QueryValue(context.Background(), "SELECT 42"); err != nil || v != "42" {
		t.Errorf("%v %q", err, v)
	}
	// A second proxy can't take a socket that's in use.
	if _, err := Listen(dir, 6433); err == nil {
		t.Error("a socket in use was taken over")
	}
}

func TestShutdownWaitsThenCloses(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	cfg, _ := pgwire.ParseConfig(dsn)
	cfg.SSLMode = "disable"
	ln, _ := Listen("127.0.0.1:0", 0)
	srv := New(Config{Upstream: cfg})
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { srv.Serve(ctx, ln); close(served) }()
	tp := &testProxy{addr: ln.Addr().(*net.TCPAddr)}
	c := connect(t, tp.via(t, dsn, "disable"))
	if srv.Active() != 1 {
		t.Errorf("active %d", srv.Active())
	}
	cancel()
	<-served
	// New connections are refused once the listener closes.
	ccfg, _ := pgwire.ParseConfig(tp.via(t, dsn, "disable"))
	ccfg.ConnectTimeout = 2 * time.Second
	if _, err := pgwire.Connect(context.Background(), ccfg); err == nil {
		t.Error("connected after shutdown began")
	}
	// The open one still works until the grace period ends.
	if _, err := c.QueryValue(context.Background(), "SELECT 1"); err != nil {
		t.Errorf("open connection broke: %v", err)
	}
	if n := srv.Shutdown(200 * time.Millisecond); n != 1 {
		t.Errorf("closed %d, want 1", n)
	}
}

func TestSCRAMChannelBindingRules(t *testing.T) {
	st := &saslState{upTLS: true}
	out, _ := st.fromServer(append(binary.BigEndian.AppendUint32(nil, 10), "SCRAM-SHA-256-PLUS\x00SCRAM-SHA-256\x00\x00"...))
	if got := string(out[4:]); got != "SCRAM-SHA-256\x00\x00" {
		t.Errorf("mechanisms after the proxy: %q", got)
	}
	initial := func(mech, data string) []byte {
		b := append([]byte(mech), 0)
		b = binary.BigEndian.AppendUint32(b, uint32(len(data)))
		return append(b, data...)
	}
	if err := st.fromClient(initial("SCRAM-SHA-256", "n,,n=,r=abc")); err != nil {
		t.Errorf("plain SCRAM refused: %v", err)
	}
	st.fromServer(append(binary.BigEndian.AppendUint32(nil, 10), "SCRAM-SHA-256\x00\x00"...))
	if err := st.fromClient(initial("SCRAM-SHA-256", "y,,n=,r=abc")); err == nil {
		t.Error("a 'y' flag over an upstream TLS link should be refused with advice")
	}
	st2 := &saslState{upTLS: false}
	st2.fromServer(append(binary.BigEndian.AppendUint32(nil, 10), "SCRAM-SHA-256\x00\x00"...))
	if err := st2.fromClient(initial("SCRAM-SHA-256", "y,,n=,r=abc")); err != nil {
		t.Errorf("'y' without upstream TLS is fine: %v", err)
	}
}

// libpq is the client most applications use, through their drivers, so
// these tests run psql and pgbench through the proxy.

func psql(t *testing.T, dsn string, args ...string) (string, error) {
	t.Helper()
	bin, err := exec.LookPath("psql")
	if err != nil {
		t.Skip("psql not found")
	}
	cmd := exec.Command(bin, append([]string{"-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-d", dsn}, args...)...)
	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=10")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestPsqlCopyAndTransactions(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	tp := startProxy(t, dsn, "prefer", false, nil)
	via := tp.via(t, dsn, "disable")
	script := `
CREATE TEMP TABLE lqo_copy(id int, v text);
COPY lqo_copy FROM STDIN;
1	one
2	two, with a comma
3	\N
\.
SELECT count(*) || ' ' || coalesce(string_agg(v, '|' ORDER BY id), '') FROM lqo_copy;
COPY (SELECT id, v FROM lqo_copy ORDER BY id) TO STDOUT;
BEGIN;
INSERT INTO lqo_copy VALUES (4, 'gone');
ROLLBACK;
SELECT count(*) FROM lqo_copy;
`
	out, err := psql(t, via, "-c", "SELECT 1")
	if err != nil {
		t.Fatalf("psql through the proxy: %v\n%s", err, out)
	}
	cmd := exec.Command("psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-d", via)
	cmd.Stdin = strings.NewReader(script)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	want := "3 one|two, with a comma\n1\tone\n2\ttwo, with a comma\n3\t\\N\n3"
	if got := strings.TrimSpace(strings.ReplaceAll(string(b), "BEGIN\nINSERT 0 1\nROLLBACK\n", "")); !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNotificationsPassThrough(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	tp := startProxy(t, dsn, "prefer", false, nil)
	out, err := psql(t, tp.via(t, dsn, "disable"), "-c", "LISTEN lqo_test", "-c", "NOTIFY lqo_test, 'hello'", "-c", "SELECT 1")
	if err != nil || !strings.Contains(out, `"lqo_test" with payload "hello"`) {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestPsqlOverTLSNeedsChannelBindingOff(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	tp := startProxy(t, dsn, "prefer", true, nil)
	via := tp.via(t, dsn, "require")
	// libpq offers channel binding by default over TLS. The proxy can't
	// pass it through, and says how to connect instead.
	out, err := psql(t, via, "-c", "SELECT 1")
	if err == nil {
		// Built without channel binding support: nothing to check.
		t.Logf("psql connected: %s", out)
	} else if !strings.Contains(out, "channel_binding=disable") {
		t.Errorf("unclear error: %s", out)
	}
	out, err = psql(t, via+"&channel_binding=disable", "-c", "SELECT 'ok'")
	if err != nil || out != "ok" {
		t.Errorf("with channel_binding=disable: %v %s", err, out)
	}
}

func TestPgbenchPipelineAndPrepared(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	bin, err := exec.LookPath("pgbench")
	if err != nil {
		t.Skip("pgbench not found")
	}
	tp := startProxy(t, dsn, "disable", false, nil)
	script := filepath.Join(t.TempDir(), "p.sql")
	os.WriteFile(script, []byte(`\set a random(1, 1000)
\startpipeline
SELECT :a + 1;
SELECT md5(:a::text);
SELECT pg_backend_pid();
\endpipeline
`), 0o600)
	for _, mode := range []string{"extended", "prepared"} {
		cmd := exec.Command(bin, "-n", "-c", "4", "-t", "200", "-M", mode, "-f", script, tp.via(t, dsn, "disable"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("pgbench -M %s: %v\n%s", mode, err, out)
		}
		if !strings.Contains(string(out), "number of transactions actually processed: 800/800") {
			t.Errorf("pgbench -M %s:\n%s", mode, out)
		}
	}
}

// countingWatcher counts what it's told about.
type countingWatcher struct {
	mu       sync.Mutex
	opened   int
	closed   int
	fromCl   map[byte]int
	fromSrv  map[byte]int
	user     string
	queryLen int
}

func (w *countingWatcher) Open(params map[string]string) Session {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.opened++
	w.user = params["user"]
	return w
}

func (w *countingWatcher) FromClient(typ byte, body, _ []byte) Action {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fromCl[typ]++
	if typ == 'Q' {
		w.queryLen += len(body)
	}
	return Action{}
}

func (w *countingWatcher) FromServer(typ byte, body []byte) Action {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fromSrv[typ]++
	return Action{}
}

func (w *countingWatcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed++
}

func TestWatcherSeesTheTraffic(t *testing.T) {
	dsn := upstreamDSN(t, "LQO_TEST_DSN")
	w := &countingWatcher{fromCl: map[byte]int{}, fromSrv: map[byte]int{}}
	tp := startProxy(t, dsn, "disable", false, w)
	c := connect(t, tp.via(t, dsn, "disable"))
	ctx := context.Background()
	c.Exec(ctx, "SELECT 1")
	c.ExecOne(ctx, "SELECT 2")
	c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	cfg, _ := pgwire.ParseConfig(dsn)
	if w.opened != 1 || w.closed != 1 || w.user != cfg.User {
		t.Errorf("opened %d closed %d user %q", w.opened, w.closed, w.user)
	}
	if w.fromCl['Q'] < 1 || w.fromCl['P'] != 1 || w.fromCl['B'] != 1 || w.fromCl['E'] != 1 || w.fromCl['S'] != 1 {
		t.Errorf("client messages: %v", fmt.Sprint(w.fromCl))
	}
	if w.fromSrv['Z'] < 2 || w.fromSrv['D'] < 2 {
		t.Errorf("server messages: %v", fmt.Sprint(w.fromSrv))
	}
	if w.fromSrv['R'] != 0 {
		t.Error("the watcher saw login messages")
	}
}
