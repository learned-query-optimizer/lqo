// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/learned-query-optimizer/lqo"
	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/learn"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/proxy"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/state"
	"github.com/learned-query-optimizer/lqo/internal/watch"
)

const serveUsage = `Usage: lqo serve --dsn SERVER [--replica REPLICA] [flags]

Runs LQO as a proxy in front of a PostgreSQL server. Applications connect to
LQO instead of the server. LQO times each query shape and, with --replica,
tries the slow shapes under each hint on the replica and proves the hints
that are clearly and consistently faster.

In shadow mode, the default, it passes every message through unchanged and
reports what it learned, at the status address and with lqo show. With
--mode active it also applies proven hints to live queries with SET LOCAL,
holds some calls out to compare, and takes a hint back when it runs slower
live. lqo switch turns hints off and on while it runs.

Shapes that read the same tables are relatives. A new shape starts its
learning from what its relatives have shown, so it tries the likely winners
first; it still needs its own results to prove a hint. --share=false turns
that off.

Applications log in to the server through LQO with their own user and
password; LQO holds no passwords. --dsn needs only the server's address and
TLS settings. --replica needs a login, as lqo advise does.

Flags:
`

type serveFlags struct {
	dsn, replica, listen, status, tlsCert, tlsKey, statePath, hintSet, mode string
	port                                                                    int
	allowPrimary, noState, quiet, share                                     bool
	minGain, minMs, budget, holdout                                         float64
	maxTime, grace, saveEvery                                               time.Duration
	session                                                                 settingList
}

func cmdServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lqo serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f serveFlags
	d := learn.Defaults
	fs.StringVar(&f.dsn, "dsn", "", "the database server, such as postgres://db1:5432/shop?sslmode=require")
	fs.StringVar(&f.replica, "replica", "", "a replica to learn on, with a login; without it LQO only measures")
	fs.StringVar(&f.listen, "listen", "127.0.0.1", "address to listen on, or a directory for a Unix socket")
	fs.IntVar(&f.port, "port", proxy.DefaultPort, "port to listen on")
	fs.StringVar(&f.status, "status", "127.0.0.1:6434", `address of the status page ("" for none)`)
	fs.StringVar(&f.mode, "mode", "shadow", "shadow (learn and report) or active (also apply proven hints)")
	fs.Float64Var(&f.holdout, "holdout", d.Holdout*100, "in active mode, percent of a hinted shape's calls run without the hint, to compare")
	fs.BoolVar(&f.share, "share", true, "start each shape's learning from what similar shapes, reading the same tables, have shown")
	fs.StringVar(&f.tlsCert, "tls-cert", "", "certificate for clients that connect with TLS")
	fs.StringVar(&f.tlsKey, "tls-key", "", "its private key")
	fs.BoolVar(&f.allowPrimary, "allow-primary", false, "let --replica be a primary server")
	fs.StringVar(&f.hintSet, "hints", d.HintSet, "hint set: standard (13 hints) or full (48)")
	fs.Float64Var(&f.minGain, "min-gain", d.MinGain*100, "percent of the time a hint must save")
	fs.Float64Var(&f.minMs, "min-ms", d.MinMs, "don't try shapes whose median time is under this many ms")
	fs.Float64Var(&f.budget, "budget", d.Budget*100, "percent of the time the learner may keep the replica busy")
	fs.DurationVar(&f.maxTime, "max-time", d.MaxTime, "stop a trial of the default plan after this long")
	fs.StringVar(&f.statePath, "state", "lqo-state.json", "state file to record findings in")
	fs.BoolVar(&f.noState, "no-state", false, "don't write the state file")
	fs.DurationVar(&f.saveEvery, "save-every", d.SaveEvery, "save the state file this often while learning")
	fs.DurationVar(&f.grace, "grace", 30*time.Second, "on shutdown, wait this long for open connections")
	fs.BoolVar(&f.quiet, "quiet", false, "log only errors")
	fs.Var(&f.session, "set", "session setting for every trial, such as search_path=app (may repeat)")
	fs.Usage = func() {
		fmt.Fprint(stderr, serveUsage)
		fs.PrintDefaults()
	}
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return usageError{fmt.Sprintf("lqo serve takes no arguments, got %q", rest[0])}
	}
	if f.dsn == "" {
		f.dsn = dsnFrom("")
	}
	if f.dsn == "" {
		fs.Usage()
		return usageError{"give the database server with --dsn"}
	}
	if f.minGain <= 0 || f.minGain >= 100 {
		return usageError{"--min-gain is a percent between 0 and 100"}
	}
	if f.budget <= 0 || f.budget > 100 {
		return usageError{"--budget is a percent between 0 and 100"}
	}
	if _, err := hints.Set(f.hintSet); err != nil {
		return usageError{err.Error()}
	}
	switch f.mode {
	case "shadow", "active":
	default:
		return usageError{fmt.Sprintf("--mode is shadow or active, not %q", f.mode)}
	}
	if f.holdout < 0 || f.holdout >= 100 {
		return usageError{"--holdout is a percent from 0 to under 100"}
	}
	if (f.tlsCert == "") != (f.tlsKey == "") {
		return usageError{"give both --tls-cert and --tls-key, or neither"}
	}
	upstream, err := pgwire.ParseConfig(f.dsn)
	if err != nil {
		return usageError{"--dsn: " + err.Error()}
	}
	var replica *pgwire.Config
	if f.replica != "" {
		if replica, err = pgwire.ParseConfig(f.replica); err != nil {
			return usageError{"--replica: " + err.Error()}
		}
	}
	if f.mode == "active" && replica == nil {
		return usageError{"--mode active needs --replica: hints are proven there before they're applied"}
	}
	var tlsConf *tls.Config
	if f.tlsCert != "" {
		pair, err := tls.LoadX509KeyPair(f.tlsCert, f.tlsKey)
		if err != nil {
			return usageError{"reading the TLS certificate: " + err.Error()}
		}
		tlsConf = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	}

	logger := log.New(stderr, "lqo: ", log.LstdFlags)
	logf := func(format string, a ...any) {
		if !f.quiet {
			logger.Printf(format, a...)
		}
	}

	col := watch.NewCollector()
	defer col.Close()
	statePath := f.statePath
	if f.noState {
		statePath = ""
	}
	var learner *learn.Learner
	if replica != nil {
		var note string
		learner, note, err = learn.New(col, learn.Options{
			Replica:      replica,
			AllowPrimary: f.allowPrimary,
			HintSet:      f.hintSet,
			Session:      f.session,
			MinGain:      f.minGain / 100,
			MinMs:        f.minMs,
			Budget:       f.budget / 100,
			MaxTime:      f.maxTime,
			StatePath:    statePath,
			SaveEvery:    f.saveEvery,
			Logf:         logf,
			Active:       f.mode == "active",
			Holdout:      f.holdout / 100,
			NoShare:      !f.share,
		})
		if err != nil {
			return err
		}
		if note != "" {
			logger.Print(note)
		}
	}

	ln, err := proxy.Listen(f.listen, f.port)
	if err != nil {
		return err
	}
	srv := proxy.New(proxy.Config{Upstream: upstream, TLS: tlsConf, Watch: col, Logf: logf})
	start := time.Now()

	var wg sync.WaitGroup
	if f.status != "" {
		sl, err := net.Listen("tcp", f.status)
		if err != nil {
			ln.Close()
			return fmt.Errorf("status page: %w", err)
		}
		hs := &http.Server{Handler: statusHandler(srv, col, learner, start, f.mode), ReadHeaderTimeout: 10 * time.Second}
		wg.Add(1)
		go func() { defer wg.Done(); hs.Serve(sl) }()
		defer func() {
			sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			hs.Shutdown(sctx)
			wg.Wait()
		}()
		logf("status page on http://%s/", sl.Addr())
	}

	lctx, stopLearning := context.WithCancel(ctx)
	learnDone := make(chan error, 1)
	if learner != nil {
		go func() {
			err := learner.Run(lctx)
			if err != nil {
				logger.Printf("learning stopped, the proxy carries on: %v", err)
			}
			learnDone <- err
		}()
	} else {
		learnDone <- nil
		logf("no --replica: measuring only, not learning")
	}
	if f.mode == "active" {
		logf("lqo %s listening on %s, in front of %s; active mode, proven hints are applied", lqo.Version, ln.Addr(), describeUpstream(upstream))
	} else {
		logf("lqo %s listening on %s, in front of %s; shadow mode, nothing is changed", lqo.Version, ln.Addr(), describeUpstream(upstream))
	}

	serveErr := srv.Serve(ctx, ln)
	// Stop learning first: that also stops applying hints, so connections
	// still open during the grace period are plain passthrough.
	stopLearning()
	learnErr := <-learnDone
	logf("shutting down; waiting up to %s for %d open connections", f.grace, srv.Active())
	if n := srv.Shutdown(f.grace); n > 0 {
		logf("closed %d connections that were still open", n)
	}
	if learnErr != nil && serveErr == nil {
		serveErr = fmt.Errorf("learning had stopped: %w", learnErr)
	}
	return serveErr
}

func describeUpstream(c *pgwire.Config) string {
	_, addr := c.Address()
	return addr + " (sslmode " + c.SSLMode + ")"
}

// statusReport is what the status page returns as JSON.
type statusReport struct {
	Version  string        `json:"lqo_version"`
	Mode     string        `json:"mode"`
	Started  time.Time     `json:"started"`
	Active   int64         `json:"connections_open"`
	Total    int64         `json:"connections_total"`
	Watch    watch.Stats   `json:"watch"`
	Learning *learn.Stats  `json:"learning,omitempty"`
	Shapes   []shapeStatus `json:"shapes"`
}

// shapeStatus is a learned shape with, in active mode, how its hint is
// doing live.
type shapeStatus struct {
	state.Learned
	Applied      bool    `json:"applied,omitempty"`
	HintedCalls  int64   `json:"hinted_calls,omitempty"`
	HintedP50Ms  float64 `json:"hinted_p50_ms,omitempty"`
	HoldoutCalls int64   `json:"holdout_calls,omitempty"`
	HoldoutP50Ms float64 `json:"holdout_p50_ms,omitempty"`
	BeforeP50Ms  float64 `json:"before_p50_ms,omitempty"`
}

func buildStatus(srv *proxy.Server, col *watch.Collector, l *learn.Learner, start time.Time, mode string) statusReport {
	r := statusReport{Version: lqo.Version, Mode: mode, Started: start.UTC(), Active: srv.Active(), Total: srv.Total(), Watch: col.Stats()}
	live := col.Snapshot()
	if l != nil {
		st := l.Stats()
		r.Learning = &st
		byKey := map[string]watch.Shape{}
		for _, s := range live {
			byKey[s.Key] = s
		}
		for _, e := range l.Shapes() {
			ss := shapeStatus{Learned: e}
			if s, ok := byKey[e.Key]; ok && s.Active {
				ss.Applied = true
				ss.HintedCalls, ss.HintedP50Ms = s.HintedCalls, s.HintedP50Ms
				ss.HoldoutCalls, ss.HoldoutP50Ms = s.HoldoutCalls, s.HoldoutP50Ms
				ss.BeforeP50Ms = s.BeforeP50Ms
			}
			r.Shapes = append(r.Shapes, ss)
		}
	} else {
		for _, s := range live {
			e := state.Learned{Key: s.Key, ID: s.ID, Database: s.Database, Normalized: s.Normalized, Calls: s.Calls, TotalMs: s.TotalMs, P50Ms: s.P50Ms, P95Ms: s.P95Ms, LastSeen: s.LastSeen, Status: "measured"}
			if len(s.Samples) > 0 {
				e.Example = s.Samples[0].Text
			}
			if s.Skip != "" {
				e.Status, e.Reason = learn.Skipped, s.Skip
			}
			r.Shapes = append(r.Shapes, shapeStatus{Learned: e})
		}
	}
	if r.Shapes == nil {
		r.Shapes = []shapeStatus{}
	}
	return r
}

func statusHandler(srv *proxy.Server, col *watch.Collector, l *learn.Learner, start time.Time, mode string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(buildStatus(srv, col, l, start, mode))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeStatusText(w, buildStatus(srv, col, l, start, mode))
	})
	// The switches. They act at once: the next query of every session
	// sees the change.
	sw := func(w http.ResponseWriter, r *http.Request, shape string, off bool) {
		if l == nil {
			http.Error(w, "no hints: lqo serve runs without --replica", http.StatusConflict)
			return
		}
		if shape == "" {
			if err := l.SwitchAll(off); err != nil {
				http.Error(w, "switched, but the state file couldn't be saved: "+err.Error(), http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, "every hint switched %s\n", onOff(off))
			return
		}
		key, err := l.SwitchShape(shape, off)
		if errors.Is(err, learn.ErrNoShape) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "switched, but the state file couldn't be saved: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "hint for %s switched %s\n", key, onOff(off))
	}
	mux.HandleFunc("POST /hints/off", func(w http.ResponseWriter, r *http.Request) { sw(w, r, "", true) })
	mux.HandleFunc("POST /hints/on", func(w http.ResponseWriter, r *http.Request) { sw(w, r, "", false) })
	mux.HandleFunc("POST /shapes/{id...}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		switch {
		case strings.HasSuffix(id, "/off"):
			sw(w, r, strings.TrimSuffix(id, "/off"), true)
		case strings.HasSuffix(id, "/on"):
			sw(w, r, strings.TrimSuffix(id, "/on"), false)
		default:
			http.NotFound(w, r)
		}
	})
	return mux
}

func onOff(off bool) string {
	if off {
		return "off"
	}
	return "on"
}

func writeStatusText(w io.Writer, r statusReport) {
	fmt.Fprintf(w, "lqo %s, %s mode, up %s\n", r.Version, r.Mode, time.Since(r.Started).Round(time.Second))
	fmt.Fprintf(w, "connections: %d open, %d in all\n", r.Active, r.Total)
	fmt.Fprintf(w, "statements measured: %d in %d shapes", r.Watch.Statements, r.Watch.Shapes)
	if r.Watch.Dropped > 0 {
		fmt.Fprintf(w, " (%d measurements dropped to keep up)", r.Watch.Dropped)
	}
	fmt.Fprintln(w)
	if r.Learning == nil {
		fmt.Fprintln(w, "learning: off (no --replica)")
	} else {
		fmt.Fprintf(w, "learning: %d rounds, %d hint trials, replica busy %s, %d proven", r.Learning.Rounds, r.Learning.Trials, (time.Duration(r.Learning.BusyMs) * time.Millisecond).Round(time.Second), r.Learning.Proven)
		if r.Learning.ReplicaDown {
			fmt.Fprintf(w, "; paused: %s", r.Learning.LastError)
		}
		fmt.Fprintln(w)
		if r.Mode == "active" {
			fmt.Fprintf(w, "applying: %s", plural(r.Learning.Applied, "hint"))
			if r.Learning.Paused != "" {
				fmt.Fprintf(w, " (none applied now: %s)", r.Learning.Paused)
			}
			if r.Learning.Reverted > 0 {
				fmt.Fprintf(w, "; %d taken back by the guard", r.Learning.Reverted)
			}
			fmt.Fprintln(w)
		}
	}
	shown := 0
	for _, s := range r.Shapes {
		if shown == 25 {
			fmt.Fprintf(w, "\n… and %d more shapes at /status\n", len(r.Shapes)-shown)
			break
		}
		shown++
		fmt.Fprintf(w, "\n%s  %s  %s\n", s.ID[:8], s.Database, sqltext.OneLine(firstNonEmpty(s.Example, s.Normalized), 70))
		fmt.Fprintf(w, "  %d calls, %s ms in all, median %s ms, p95 %s ms\n", s.Calls, watch.FormatMs(s.TotalMs), watch.FormatMs(s.P50Ms), watch.FormatMs(s.P95Ms))
		switch s.Status {
		case learn.Proven:
			fmt.Fprintf(w, "  proven: %q, about %.0f%% faster on the replica", s.Proven, s.Gain*100)
			if s.RoundsToProof > 0 {
				fmt.Fprintf(w, ", after %s", plural(s.RoundsToProof, "round"))
				if s.HeadStart > 0 {
					fmt.Fprintf(w, " with a head start from %s", plural(s.HeadStart, "similar shape"))
				}
			}
			fmt.Fprintln(w)
			for _, st := range s.Settings {
				fmt.Fprintf(w, "    SET LOCAL %s = %s;\n", st.Name, st.Value)
			}
			switch {
			case s.Disabled:
				fmt.Fprintln(w, "  switched off by hand")
			case s.Applied:
				fmt.Fprintf(w, "  applied: %d calls, median %s ms", s.HintedCalls, watch.FormatMs(s.HintedP50Ms))
				if s.HoldoutCalls > 0 {
					fmt.Fprintf(w, "; %d held out, median %s ms", s.HoldoutCalls, watch.FormatMs(s.HoldoutP50Ms))
				}
				fmt.Fprintf(w, "; %s ms before\n", watch.FormatMs(s.BeforeP50Ms))
			}
		case learn.Learning:
			if s.Rounds > 0 {
				fmt.Fprintf(w, "  learning: %s on %s", plural(s.Rounds, "round"), plural(s.Texts, "text"))
				if s.HeadStart > 0 {
					fmt.Fprintf(w, ", with a head start from %s", plural(s.HeadStart, "similar shape"))
				}
				fmt.Fprintln(w)
			} else {
				fmt.Fprintln(w, "  learning: not tried yet")
			}
		default:
			fmt.Fprintf(w, "  %s", s.Status)
			if s.Reason != "" {
				fmt.Fprintf(w, ": %s", s.Reason)
			}
			fmt.Fprintln(w)
		}
	}
}

// plural says "1 round" or "3 rounds".
func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// showServe prints what lqo serve has learned, for lqo show.
func showServe(w io.Writer, path string, f *state.File) {
	list := make([]state.Learned, 0, len(f.Serve))
	proven := 0
	for _, e := range f.Serve {
		list = append(list, *e)
		if e.Status == learn.Proven {
			proven++
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if (list[i].Status == learn.Proven) != (list[j].Status == learn.Proven) {
			return list[i].Status == learn.Proven
		}
		return list[i].TotalMs > list[j].TotalMs
	})
	fmt.Fprintf(w, "%s, updated %s: %d shapes seen by lqo serve, %d with a proven hint\n", path, f.Updated.Format("2006-01-02 15:04 UTC"), len(list), proven)
	for _, s := range list {
		if s.Status == learn.Skipped {
			continue
		}
		fmt.Fprintf(w, "\n%s  %s  %s\n", s.ID[:8], s.Database, sqltext.OneLine(firstNonEmpty(s.Example, s.Normalized), 70))
		fmt.Fprintf(w, "  %d calls, median %s ms live\n", s.Calls, watch.FormatMs(s.P50Ms))
		switch s.Status {
		case learn.Proven:
			fmt.Fprintf(w, "  proven %s: %q, about %.0f%% faster on the replica\n", s.ProvenAt.Format("2006-01-02 15:04 UTC"), s.Proven, s.Gain*100)
			for _, st := range s.Settings {
				fmt.Fprintf(w, "    SET LOCAL %s = %s;\n", st.Name, st.Value)
			}
		case learn.Learning:
			fmt.Fprintf(w, "  learning: %d rounds on %d texts\n", s.Rounds, s.Texts)
		default:
			fmt.Fprintf(w, "  %s: %s\n", s.Status, s.Reason)
		}
	}
}

const switchUsage = `Usage: lqo switch off|on [SHAPE] [--status ADDRESS]

Turns the hints a running lqo serve applies off or on: every hint, or one
shape's, named by its ID or the start of it (at least 4 characters). The
change takes effect on the next query and is saved in the state file, so it
holds across restarts.

Flags:
`

func cmdSwitch(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lqo switch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	status := fs.String("status", "127.0.0.1:6434", "status address of the running lqo serve")
	fs.Usage = func() {
		fmt.Fprint(stderr, switchUsage)
		fs.PrintDefaults()
	}
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 || len(rest) > 2 || (rest[0] != "off" && rest[0] != "on") {
		fs.Usage()
		return usageError{"say off or on, and optionally a shape"}
	}
	path := "/hints/" + rest[0]
	if len(rest) == 2 {
		path = "/shapes/" + rest[1] + "/" + rest[0]
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post("http://"+*status+path, "text/plain", nil)
	if err != nil {
		return fmt.Errorf("couldn't reach lqo serve at %s: %w", *status, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusConflict {
			return usageError{msg}
		}
		return errors.New(msg)
	}
	fmt.Fprint(stdout, string(body))
	return nil
}
