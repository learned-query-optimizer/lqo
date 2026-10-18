// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/learned-query-optimizer/lqo"
	"github.com/learned-query-optimizer/lqo/internal/advisor"
	"github.com/learned-query-optimizer/lqo/internal/state"
)

func runCLI(args ...string) (int, string, string) {
	var out, errOut strings.Builder
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersionAndHelp(t *testing.T) {
	code, out, _ := runCLI("version")
	if code != 0 || !strings.Contains(out, "lqo "+lqo.Version) {
		t.Errorf("%d %q", code, out)
	}
	if code, out, _ := runCLI("help"); code != 0 || !strings.Contains(out, "lqo advise") {
		t.Errorf("help: %d", code)
	}
	if code, _, _ := runCLI(); code != exitRefused {
		t.Error("no command should exit 2")
	}
	if code, _, errOut := runCLI("frobnicate"); code != exitRefused || !strings.Contains(errOut, "unknown command") {
		t.Error("unknown command")
	}
}

func TestHints(t *testing.T) {
	code, out, _ := runCLI("hints")
	if code != 0 || strings.Count(out, "\n") != 13 || !strings.Contains(out, "SET LOCAL enable_nestloop = off;") {
		t.Errorf("%d\n%s", code, out)
	}
	code, out, _ = runCLI("hints", "--hints", "full")
	if code != 0 || strings.Count(out, "\n") != 48 {
		t.Errorf("full: %d lines", strings.Count(out, "\n"))
	}
	if code, _, _ := runCLI("hints", "--hints", "nope"); code != exitRefused {
		t.Error("bad set accepted")
	}
}

func TestAdviseUsageErrors(t *testing.T) {
	if code, _, _ := runCLI("advise"); code != exitRefused {
		t.Error("advise without a workload should exit 2")
	}
	if code, _, _ := runCLI("advise", "--report", "pdf", "x.sql"); code != exitRefused {
		t.Error("bad report format accepted")
	}
	if code, _, _ := runCLI("advise", "--min-gain", "150", "x.sql"); code != exitRefused {
		t.Error("bad gain accepted")
	}
	if code, _, _ := runCLI("advise", "-h"); code != 0 {
		t.Error("-h should exit 0")
	}
	if code, _, errOut := runCLI("advise", filepath.Join(t.TempDir(), "missing.sql")); code != exitError || !strings.Contains(errOut, "missing.sql") {
		t.Errorf("missing file: %d %s", code, errOut)
	}
}

func TestDemoWorkloadOnlyAndShow(t *testing.T) {
	dir := t.TempDir()
	wl := filepath.Join(dir, "demo.sql")
	code, out, _ := runCLI("demo", "--workload-only", "--workload", wl)
	if code != 0 || !strings.Contains(out, wl) {
		t.Fatalf("%d %s", code, out)
	}
	b, _ := os.ReadFile(wl)
	if !strings.Contains(string(b), "lqo_demo.customers") {
		t.Error("workload not written")
	}
	sp := filepath.Join(dir, "state.json")
	code, out, _ = runCLI("show", "--state", sp)
	if code != 0 || !strings.Contains(out, "holds nothing yet") {
		t.Errorf("empty show: %d %s", code, out)
	}
	f := state.New()
	f.Put(&state.Entry{ID: "891f0819ce6bf4ec", Example: "SELECT 1", Verdict: advisor.Faster, Hint: "no nested loop", BaselineMs: 10, HintMs: 5, Gain: 0.5})
	f.Save(sp)
	code, out, _ = runCLI("show", "--state", sp)
	if code != 0 || !strings.Contains(out, `faster with "no nested loop"`) {
		t.Errorf("show: %d %s", code, out)
	}
	code, out, _ = runCLI("show", "--json", "--state", sp)
	if code != 0 || !json.Valid([]byte(out)) {
		t.Errorf("show --json: %d", code)
	}
}

// TestAdviseEndToEnd runs the whole command against LQO_TEST_DSN.
func TestAdviseEndToEnd(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	dir := t.TempDir()
	wl := filepath.Join(dir, "w.sql")
	os.WriteFile(wl, []byte(`
SELECT count(*) FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid WHERE c.relkind = 'r';
SELECT count(*) FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid WHERE c.relkind = 'v';
DELETE FROM pg_class WHERE false;
`), 0o644)
	sp := filepath.Join(dir, "s.json")
	if code, _, errOut := runCLI("advise", "--dsn", dsn, "--state", sp, wl); code != exitRefused || !strings.Contains(errOut, "primary") {
		t.Skipf("the test server isn't a primary, or: %s", errOut)
	}
	rp := filepath.Join(dir, "r.json")
	code, _, errOut := runCLI("advise", "--allow-primary", "--quiet", "--repeat", "1", "--dsn", dsn, "--state", sp, "--report", "json", "--out", rp, wl)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	b, err := os.ReadFile(rp)
	if err != nil {
		t.Fatal(err)
	}
	var rep advisor.Report
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Statements != 3 || len(rep.Shapes) != 2 || rep.LQOVersion != lqo.Version {
		t.Errorf("statements %d, shapes %d", rep.Statements, len(rep.Shapes))
	}
	f, err := state.Load(sp)
	if err != nil || len(f.Shapes) != 1 {
		t.Errorf("state: %v, %d shapes", err, len(f.Shapes))
	}
	if code, out, _ := runCLI("report", "--report", "markdown", rp); code != 0 || !strings.Contains(out, "## Not tested (1)") {
		t.Errorf("report from JSON: %d\n%s", code, out)
	}
	if code, _, _ := runCLI("report", wl); code != exitError {
		t.Error("a SQL file isn't a JSON report")
	}
	code, out, _ := runCLI("advise", "--allow-primary", "--quiet", "--repeat", "1", "--no-state", "--dsn", dsn, "--report", "markdown", wl)
	if code != 0 || !strings.HasPrefix(out, "# LQO advisor report") {
		t.Errorf("markdown: %d", code)
	}
}
