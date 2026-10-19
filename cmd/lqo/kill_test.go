// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package main

import (
	"bufio"
	"context"
	"math/rand/v2"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/state"
)

// TestServeSurvivesSIGKILL kills a running lqo serve with SIGKILL at random
// moments while it learns and saves, starts it again each time, and checks
// that the state file is always whole and that what was learned carries on.
func TestServeSurvivesSIGKILL(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	if testing.Short() {
		t.Skip("short")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "lqo")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	sp := filepath.Join(dir, "state.json")
	listening := regexp.MustCompile(`listening on (\S+),`)
	const kills = 10
	lastRounds := 0
	for i := 0; i < kills; i++ {
		cmd := exec.Command(bin, "serve", "--dsn", dsn, "--replica", dsn, "--allow-primary",
			"--listen", "127.0.0.1", "--port", "0", "--status", "", "--min-ms", "0",
			"--budget", "100", "--save-every", "1ms", "--state", sp)
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		addr := ""
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := listening.FindStringSubmatch(sc.Text()); m != nil {
				addr = m[1]
				break
			}
		}
		go func() {
			for sc.Scan() {
			}
		}()
		if addr == "" {
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatal("serve didn't start")
		}
		u, _ := url.Parse(dsn)
		u.Host = addr
		cfg, _ := pgwire.ParseConfig(u.String())
		conn, err := pgwire.Connect(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 4; j++ {
			if _, err := conn.ExecOne(context.Background(), "SELECT count(*) FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid WHERE a.attnum > "+strconv.Itoa(j)); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(time.Duration(1200+rand.IntN(1500)) * time.Millisecond)
		cmd.Process.Kill() // SIGKILL
		cmd.Wait()
		conn.Close()

		f, err := state.Load(sp)
		if err != nil {
			t.Fatalf("after kill %d the state file is damaged: %v", i+1, err)
		}
		rounds := 0
		for _, e := range f.Serve {
			rounds += e.Rounds
		}
		if rounds < lastRounds {
			t.Fatalf("after kill %d the learned rounds went back from %d to %d", i+1, lastRounds, rounds)
		}
		lastRounds = rounds
	}
	if lastRounds == 0 {
		t.Error("nothing was learned across the restarts")
	}
	t.Logf("%d kills, state whole each time, %d rounds learned in all", kills, lastRounds)
}
