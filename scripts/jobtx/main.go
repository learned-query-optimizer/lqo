// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Command jobtx replays the Join Order Benchmark through lqo serve in
// active mode the way an ORM sends it: each query inside a transaction of
// its own (BEGIN, the query, a statement that reads the planner settings,
// COMMIT), all with the extended protocol. The hints are the ones lqo
// advise proved in a recorded JOB run, given to lqo serve as proven shapes.
// Each round runs every query once with LQO's hints switched off and once
// with them on, in alternating order, and the statement after each query
// checks that no setting of the hint reached it.
//
//	go run ./scripts/jobtx --dsn postgres://... --lqo ./lqo \
//	    --report test/results/job-join-hints/job.json --workload job.sql --out test/results/job-tx
//
// scripts/job.sh runs it with JOB_TX=1, after loading the data.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/state"
)

// check reads every setting a hint can change, as the statement after
// each query in its transaction.
var check string

func init() {
	seen := map[string]bool{"plan_cache_mode": true}
	names := []string{"plan_cache_mode"}
	for _, a := range hints.Full() {
		for _, s := range a.Settings {
			if !seen[s.Name] {
				seen[s.Name] = true
				names = append(names, s.Name)
			}
		}
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = "'" + n + "=' || current_setting('" + n + "')"
	}
	check = "SELECT " + strings.Join(parts, " || ' ' || ")
}

type report struct {
	Version  string `json:"lqo_version"`
	Database string `json:"database"`
	Shapes   []struct {
		ID      string `json:"id"`
		Verdict string `json:"verdict"`
		Example string `json:"example"`
		Best    *struct {
			Name     string          `json:"name"`
			Settings []hints.Setting `json:"settings"`
			Comment  string          `json:"hint_comment"`
			Gain     float64         `json:"gain"`
		} `json:"best"`
	} `json:"shapes"`
}

type query struct {
	Name  string `json:"name"`
	Shape string `json:"shape"`
	Hint  string `json:"hint,omitempty"`
	// Hinted counts the calls with hints on that lqo serve hinted.
	Hinted int       `json:"hinted,omitempty"`
	Off    []float64 `json:"off_ms"`
	On     []float64 `json:"on_ms"`
	text   string
}

type result struct {
	Version     string    `json:"lqo_version"`
	Recorded    time.Time `json:"recorded"`
	Server      string    `json:"server"`
	Report      string    `json:"hints_from"`
	Rounds      int       `json:"rounds"`
	Queries     []*query  `json:"queries"`
	Proven      int       `json:"proven_shapes"`
	HintedCalls int64     `json:"hinted_calls"`
	WantHinted  int       `json:"hinted_calls_expected"`
	Leaks       int       `json:"leaks"`
	OffMs       float64   `json:"pass_off_ms"`
	OnMs        float64   `json:"pass_on_ms"`
	HintedOffMs float64   `json:"hinted_queries_off_ms"`
	HintedOnMs  float64   `json:"hinted_queries_on_ms"`
	// Reverted is what lqo serve's guard took back during the run, and why.
	Reverted map[string]string `json:"reverted,omitempty"`
}

func main() {
	dsn := flag.String("dsn", "", "the database, with the JOB data loaded")
	bin := flag.String("lqo", "lqo", "the lqo binary")
	reportPath := flag.String("report", "test/results/job-join-hints/job.json", "lqo advise's JSON report, for the hints")
	workload := flag.String("workload", "", "the JOB queries, one file, each after a -- name.sql line")
	out := flag.String("out", "test/results/job-tx", "folder to write the results to")
	rounds := flag.Int("rounds", 3, "rounds, each running every query with the hints off and on")
	port := flag.Int("port", 6533, "port for lqo serve")
	flag.Parse()
	if err := run(*dsn, *bin, *reportPath, *workload, *out, *rounds, *port); err != nil {
		fmt.Fprintln(os.Stderr, "jobtx:", err)
		os.Exit(1)
	}
}

func run(dsn, bin, reportPath, workload, out string, rounds, port int) error {
	ctx := context.Background()
	var rep report
	b, err := os.ReadFile(reportPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		return err
	}
	cfg, err := pgwire.ParseConfig(dsn)
	if err != nil {
		return err
	}
	// The proven shapes, as lqo serve keeps them.
	tmp, err := os.MkdirTemp("", "jobtx")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if os.Getenv("JOBTX_KEEP") != "" {
		fmt.Fprintln(os.Stderr, "state in", tmp)
	}
	f := state.New()
	hintOf := map[string]string{}
	for _, s := range rep.Shapes {
		if s.Verdict != "faster" || s.Best == nil {
			continue
		}
		if got := sqltext.Fingerprint(s.Example); got != s.ID {
			return fmt.Errorf("shape %s: the example's shape is %s", s.ID, got)
		}
		key := cfg.Database + "/" + s.ID
		f.Serve[key] = &state.Learned{Key: key, ID: s.ID, Database: cfg.Database, Status: "proven",
			Proven: s.Best.Name, Settings: s.Best.Settings, Comment: s.Best.Comment, Gain: s.Best.Gain, Arms: map[string]*state.Arm{}}
		hintOf[s.ID] = s.Best.Name
	}
	statePath := filepath.Join(tmp, "state.json")
	if err := f.Save(statePath); err != nil {
		return err
	}

	// The queries.
	wb, err := os.ReadFile(workload)
	if err != nil {
		return err
	}
	nameRE := regexp.MustCompile(`^--\s*(\S+)\.sql`)
	var qs []*query
	for _, piece := range sqltext.Split(string(wb)) {
		name := ""
		if m := nameRE.FindStringSubmatch(piece); m != nil {
			name = m[1]
		}
		text := sqltext.TrimLeadingComments(piece)
		id := sqltext.Fingerprint(text)
		qs = append(qs, &query{Name: name, Shape: id, Hint: hintOf[id], text: text})
	}

	// lqo serve in active mode. It learns nothing new (--min-ms) and holds
	// nothing out, so every call of a proven shape is hinted.
	status := fmt.Sprintf("127.0.0.1:%d", port+1)
	cmd := exec.Command(bin, "serve", "--dsn", dsn, "--replica", dsn, "--allow-primary", "--mode", "active",
		"--holdout", "0", "--min-ms", "1e12", "--state", statePath, "--listen", "127.0.0.1",
		"--port", fmt.Sprint(port), "--status", status)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() }()
	u, _ := url.Parse(dsn)
	u.Host = fmt.Sprintf("127.0.0.1:%d", port)
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	pcfg, _ := pgwire.ParseConfig(u.String())
	var app *pgwire.Conn
	for i := 0; i < 100; i++ {
		if app, err = pgwire.Connect(ctx, pcfg); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	defer app.Close()
	server, _ := app.QueryValue(ctx, "SELECT version()")
	want, err := app.QueryValue(ctx, check)
	if err != nil {
		return err
	}
	sw := func(on bool) error {
		path := "/hints/off"
		if on {
			path = "/hints/on"
		}
		resp, err := http.Post("http://"+status+path, "text/plain", nil)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("%s: %s", path, resp.Status)
		}
		return nil
	}
	exec1 := func(sql string) error {
		_, err := app.ExecOne(ctx, sql)
		return err
	}
	// Hints are applied once the learner has reached the replica.
	applied := 0
	for i := 0; i < 300 && applied < len(hintOf); i++ {
		time.Sleep(100 * time.Millisecond)
		if applied, err = appliedShapes(status); err != nil {
			return err
		}
	}
	if applied < len(hintOf) {
		return fmt.Errorf("lqo serve applies %d of the %d proven shapes", applied, len(hintOf))
	}
	res := &result{Server: server, Report: reportPath, Rounds: rounds, Queries: qs, Proven: len(hintOf), Recorded: time.Now().UTC()}
	v, _ := exec.Command(bin, "version").Output()
	res.Version = string(v)
	for r := 0; r < rounds; r++ {
		for i, qq := range qs {
			order := []bool{false, true}
			if (r+i)%2 == 1 {
				order = []bool{true, false}
			}
			for _, on := range order {
				if err := sw(on); err != nil {
					return err
				}
				if err := exec1("BEGIN"); err != nil {
					return err
				}
				// lqo serve's count of the shape's hinted calls, before.
				var before int64
				if on && qq.Hint != "" {
					if before, err = hintedCalls(status, qq.Shape); err != nil {
						return err
					}
				}
				t0 := time.Now()
				if err := exec1(qq.text); err != nil {
					return fmt.Errorf("%s: %v", qq.Name, err)
				}
				ms := float64(time.Since(t0).Microseconds()) / 1000
				got, err := app.ExecOne(ctx, check)
				if err != nil {
					return err
				}
				if v := *got[0].Rows[0][0]; v != want {
					res.Leaks++
					fmt.Fprintf(os.Stderr, "%s: the statement after it saw %s\n", qq.Name, v)
				}
				if err := exec1("COMMIT"); err != nil {
					return err
				}
				if on {
					qq.On = append(qq.On, ms)
					if qq.Hint != "" {
						n, err := hintedCalls(status, qq.Shape)
						if err != nil {
							return err
						}
						if n == before+1 {
							res.HintedCalls++
							qq.Hinted++
						} else {
							fmt.Fprintf(os.Stderr, "%s: lqo serve's count of hinted calls went from %d to %d\n", qq.Name, before, n)
						}
					}
				} else {
					qq.Off = append(qq.Off, ms)
				}
			}
		}
		fmt.Fprintf(os.Stderr, "round %d of %d done\n", r+1, rounds)
	}
	if res.Reverted, err = reverted(status); err != nil {
		return err
	}
	for _, qq := range qs {
		off, on := median(qq.Off), median(qq.On)
		res.OffMs += off
		res.OnMs += on
		if qq.Hint != "" {
			res.WantHinted += rounds
			res.HintedOffMs += off
			res.HintedOnMs += on
		}
	}
	return write(out, res)
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func pct(a, b float64) string {
	if a == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%+.0f%%", (b-a)/a*100)
}

func write(dir string, res *result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "job-tx.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	var w strings.Builder
	fmt.Fprintf(&w, "# Join Order Benchmark in ORM-style transactions through lqo serve --mode active\n\n")
	fmt.Fprintf(&w, "- **LQO:** %s\n- **Server:** %s\n- **Hints:** the %d shapes proven in %s\n", strings.TrimSpace(res.Version), res.Server, res.Proven, res.Report)
	fmt.Fprintf(&w, "- **Each call:** BEGIN; the query; a statement reading every planner setting a hint can change; COMMIT; extended protocol\n")
	fmt.Fprintf(&w, "- **Rounds:** %d, each query once with LQO's hints off and once on, alternating which goes first; times are medians\n\n", res.Rounds)
	fmt.Fprintf(&w, "## Result\n\n")
	fmt.Fprintf(&w, "- Calls hinted, as lqo serve counts them: %d of the %d calls of proven shapes made with hints on\n", res.HintedCalls, res.WantHinted)
	fmt.Fprintf(&w, "- Statements after a hinted query that saw any of its settings: %d\n", res.Leaks)
	fmt.Fprintf(&w, "- One pass over all %d queries: %.1f s with hints off, %.1f s on (%s)\n", len(res.Queries), res.OffMs/1000, res.OnMs/1000, pct(res.OffMs, res.OnMs))
	fmt.Fprintf(&w, "- The queries of proven shapes alone: %.1f s off, %.1f s on (%s)\n\n", res.HintedOffMs/1000, res.HintedOnMs/1000, pct(res.HintedOffMs, res.HintedOnMs))
	if len(res.Reverted) > 0 {
		fmt.Fprintf(&w, "## Taken back by the guard during the run\n\n")
		ids := make([]string, 0, len(res.Reverted))
		for id := range res.Reverted {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Fprintf(&w, "- `%s`: %s\n", id[:8], res.Reverted[id])
		}
		fmt.Fprintln(&w)
	}
	fmt.Fprintf(&w, "## Queries with a hint\n\n| Query | Shape | Hint | Hinted calls | Off (ms) | On (ms) | Change |\n|---|---|---|---:|---:|---:|---:|\n")
	for _, q := range res.Queries {
		if q.Hint == "" {
			continue
		}
		off, on := median(q.Off), median(q.On)
		fmt.Fprintf(&w, "| %s | `%s` | %s | %d of %d | %.1f | %.1f | %s |\n", q.Name, q.Shape[:8], q.Hint, q.Hinted, len(q.On), off, on, pct(off, on))
	}
	return os.WriteFile(filepath.Join(dir, "job-tx.md"), []byte(w.String()), 0o644)
}

// appliedShapes is how many shapes lqo serve applies a hint to now.
func appliedShapes(status string) (int, error) {
	resp, err := http.Get("http://" + status + "/status")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var st struct {
		Learning struct {
			Applied int `json:"applied"`
		} `json:"learning"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return 0, err
	}
	return st.Learning.Applied, nil
}

// hintedCalls is the count lqo serve keeps of a shape's hinted calls.
func hintedCalls(status, id string) (int64, error) {
	resp, err := http.Get("http://" + status + "/status")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var st struct {
		Shapes []struct {
			ID          string `json:"id"`
			HintedCalls int64  `json:"hinted_calls"`
		} `json:"shapes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return 0, err
	}
	for _, s := range st.Shapes {
		if s.ID == id {
			return s.HintedCalls, nil
		}
	}
	return 0, nil
}

// reverted lists the shapes lqo serve's guard took back, with its reason.
func reverted(status string) (map[string]string, error) {
	resp, err := http.Get("http://" + status + "/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var st struct {
		Shapes []struct {
			ID       string `json:"id"`
			Reverted string `json:"reverted"`
		} `json:"shapes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, s := range st.Shapes {
		if s.Reverted != "" {
			out[s.ID] = s.Reverted
		}
	}
	return out, nil
}
