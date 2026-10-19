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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
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
