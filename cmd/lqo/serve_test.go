// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/learn"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/state"
)

// syncBuffer is a bytes.Buffer safe to write from the server's goroutines
// while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuffer) waitFor(t *testing.T, re *regexp.Regexp) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := re.FindStringSubmatch(s.String()); m != nil {
			return m
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never appeared in:\n%s", re, s.String())
	return nil
}

type running struct {
	stderr *syncBuffer
	cancel context.CancelFunc
	done   chan error
	addr   string
	status string
}

func startServe(t *testing.T, args ...string) *running {
	t.Helper()
	r := &running{stderr: &syncBuffer{}, done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	args = append([]string{"--listen", "127.0.0.1", "--port", "0", "--status", "127.0.0.1:0", "--grace", "1s"}, args...)
	go func() { r.done <- cmdServe(ctx, args, &bytes.Buffer{}, r.stderr) }()
	r.status = r.stderr.waitFor(t, regexp.MustCompile(`status page on http://(\S+)/`))[1]
	r.addr = r.stderr.waitFor(t, regexp.MustCompile(`listening on (\S+),`))[1]
	t.Cleanup(func() { r.stop(t) })
	return r
}

func (r *running) stop(t *testing.T) {
	if r.cancel == nil {
		return
	}
	r.cancel()
	r.cancel = nil
	select {
	case err := <-r.done:
		if err != nil {
			t.Errorf("serve returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Error("serve didn't stop")
	}
}

func (r *running) via(dsn string) string {
	u, _ := url.Parse(dsn)
	u.Host = r.addr
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

func (r *running) statusJSON(t *testing.T) statusReport {
	t.Helper()
	resp, err := http.Get("http://" + r.status + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s statusReport
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServeUsageErrors(t *testing.T) {
	if code, _, _ := runCLI("serve"); code != exitRefused {
		t.Error("serve without --dsn should exit 2")
	}
	if code, _, _ := runCLI("serve", "--dsn", "host=x", "--tls-cert", "a.crt"); code != exitRefused {
		t.Error("a certificate without its key should exit 2")
	}
	if code, _, _ := runCLI("serve", "--dsn", "host=x", "--budget", "0"); code != exitRefused {
		t.Error("a zero budget should exit 2")
	}
}

func TestServeMeasuresAndLearns(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	sp := filepath.Join(t.TempDir(), "state.json")
	r := startServe(t, "--dsn", dsn, "--replica", dsn, "--allow-primary", "--min-ms", "0", "--budget", "100", "--state", sp)
	cfg, _ := pgwire.ParseConfig(r.via(dsn))
	c, err := pgwire.Connect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := c.ExecOne(ctx, "SELECT count(*) FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid WHERE c.relkind = 'r'"); err != nil {
			t.Fatal(err)
		}
	}
	c.Exec(ctx, "SELECT 1")
	c.Close()

	// The learner picks the shape up and tries hints on it.
	deadline := time.Now().Add(30 * time.Second)
	var st statusReport
	for time.Now().Before(deadline) {
		st = r.statusJSON(t)
		if st.Learning != nil && st.Learning.Rounds > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st.Mode != "shadow" || st.Total != 1 || st.Watch.Statements < 6 {
		t.Errorf("status: %+v", st)
	}
	if st.Learning == nil || st.Learning.Rounds == 0 {
		t.Fatalf("no learning rounds: %+v\n%s", st.Learning, r.stderr.String())
	}
	resp, err := http.Get("http://" + r.status + "/")
	if err != nil {
		t.Fatal(err)
	}
	var page bytes.Buffer
	page.ReadFrom(resp.Body)
	resp.Body.Close()
	if !strings.Contains(page.String(), "shadow mode") || !strings.Contains(page.String(), "pg_attribute") {
		t.Errorf("status page:\n%s", page.String())
	}

	r.stop(t)
	f, err := state.Load(sp)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range f.Serve {
		if strings.Contains(e.Normalized, "pg_attribute") && e.Rounds > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("the state file doesn't hold the learned shape: %+v", f.Serve)
	}
	code, out, _ := runCLI("show", "--state", sp)
	if code != 0 || !strings.Contains(out, "seen by lqo serve") {
		t.Errorf("show: %d\n%s", code, out)
	}
}

func TestServeActiveWithSwitchesAndPgbench(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	// The probe's answer shows whether the hint was applied. It sleeps
	// 10 ms so the guard, which compares hinted calls with earlier ones,
	// isn't judging LQO's own fraction of a millisecond.
	const probe = "SELECT current_setting('enable_nestloop') AS v FROM pg_sleep(0.01)"
	cfg, _ := pgwire.ParseConfig(dsn)
	sp := filepath.Join(t.TempDir(), "state.json")
	f := state.New()
	key := cfg.Database + "/" + sqltext.Fingerprint(probe)
	f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(probe), Database: cfg.Database, Status: learn.Proven,
		Proven: "no nested loop", Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}}, Arms: map[string]*state.Arm{}}
	if err := f.Save(sp); err != nil {
		t.Fatal(err)
	}
	r := startServe(t, "--dsn", dsn, "--replica", dsn, "--allow-primary", "--mode", "active", "--holdout", "0", "--min-ms", "1000000", "--state", sp)
	pcfg, _ := pgwire.ParseConfig(r.via(dsn))
	c, err := pgwire.Connect(context.Background(), pcfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	get := func() string {
		v, err := c.QueryValue(context.Background(), probe)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// The learner publishes the hint as it starts.
	deadline := time.Now().Add(10 * time.Second)
	for get() != "off" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if get() != "off" {
		t.Fatalf("the proven hint isn't applied\n%s", r.stderr.String())
	}
	if code, out, errOut := runCLI("switch", "off", "--status", r.status); code != 0 || !strings.Contains(out, "off") {
		t.Fatalf("switch off: %d %s %s", code, out, errOut)
	}
	if get() != "on" {
		t.Error("applied after lqo switch off")
	}
	runCLI("switch", "on", "--status", r.status)
	if get() != "off" {
		t.Error("not applied after lqo switch on")
	}
	if code, _, _ := runCLI("switch", "off", "zzzzzzzz", "--status", r.status); code != exitRefused {
		t.Error("an unknown shape should exit 2")
	}
	if code, _, _ := runCLI("switch", "off", sqltext.Fingerprint(probe)[:8], "--status", r.status); code != 0 || get() != "on" {
		t.Error("switching one shape off didn't take")
	}
	runCLI("switch", "on", sqltext.Fingerprint(probe)[:8], "--status", r.status)

	// pgbench through the active proxy, every protocol, pipelines too,
	// with the probe hinted wherever it stands alone.
	bin, err := exec.LookPath("pgbench")
	if err != nil {
		t.Skip("pgbench not found")
	}
	script := filepath.Join(t.TempDir(), "p.sql")
	os.WriteFile(script, []byte("\\set a random(1, 100)\n"+probe+";\nSELECT :a + 1;\n\\startpipeline\n"+probe+";\nSELECT md5(:a::text);\n\\endpipeline\n"), 0o600)
	for _, mode := range []string{"simple", "extended", "prepared"} {
		if mode == "simple" {
			// pipelines need the extended protocol
			os.WriteFile(script, []byte("\\set a random(1, 100)\n"+probe+";\nSELECT :a + 1;\n"), 0o600)
		} else {
			os.WriteFile(script, []byte("\\set a random(1, 100)\n"+probe+";\nSELECT :a + 1;\n\\startpipeline\n"+probe+";\nSELECT md5(:a::text);\n\\endpipeline\n"), 0o600)
		}
		out, err := exec.Command(bin, "-n", "-c", "4", "-t", "100", "-M", mode, "-f", script, r.via(dsn)).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "processed: 400/400") {
			t.Fatalf("pgbench -M %s through active LQO: %v\n%s", mode, err, out)
		}
	}
	st := r.statusJSON(t)
	hinted := int64(0)
	for _, s := range st.Shapes {
		hinted += s.HintedCalls
	}
	if st.Mode != "active" || hinted < 1200 {
		t.Errorf("mode %s, %d hinted calls", st.Mode, hinted)
	}
	if get() != "off" {
		t.Error("the session lost its way after pgbench")
	}
	if v, _ := c.QueryValue(context.Background(), "SHOW enable_nestloop"); v != "on" {
		t.Error("the hint leaked")
	}
}
