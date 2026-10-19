// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
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
LQO instead of the server; LQO passes every message through unchanged. On
the side it times each query shape and, with --replica, tries the slow
shapes under each hint on the replica and proves the hints that are clearly
and consistently faster. In this version it changes nothing: it learns and
reports, at the status address and with lqo show.

Applications log in to the server through LQO with their own user and
password; LQO holds no passwords. --dsn needs only the server's address and
TLS settings. --replica needs a login, as lqo advise does.

Flags:
`

type serveFlags struct {
	dsn, replica, listen, status, tlsCert, tlsKey, statePath, hintSet string
	port                                                              int
	allowPrimary, noState, quiet                                      bool
	minGain, minMs, budget                                            float64
	maxTime, grace, saveEvery                                         time.Duration
	session                                                           settingList
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
		hs := &http.Server{Handler: statusHandler(srv, col, learner, start), ReadHeaderTimeout: 10 * time.Second}
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
	logf("lqo %s listening on %s, in front of %s; shadow mode, nothing is changed", lqo.Version, ln.Addr(), describeUpstream(upstream))

	serveErr := srv.Serve(ctx, ln)
	logf("shutting down; waiting up to %s for %d open connections", f.grace, srv.Active())
	if n := srv.Shutdown(f.grace); n > 0 {
		logf("closed %d connections that were still open", n)
	}
	stopLearning()
	if err := <-learnDone; err != nil && serveErr == nil {
		serveErr = fmt.Errorf("learning had stopped: %w", err)
	}
	return serveErr
}

func describeUpstream(c *pgwire.Config) string {
	_, addr := c.Address()
	return addr + " (sslmode " + c.SSLMode + ")"
}

// statusReport is what the status page returns as JSON.
type statusReport struct {
	Version  string          `json:"lqo_version"`
	Mode     string          `json:"mode"`
	Started  time.Time       `json:"started"`
	Active   int64           `json:"connections_open"`
	Total    int64           `json:"connections_total"`
	Watch    watch.Stats     `json:"watch"`
	Learning *learn.Stats    `json:"learning,omitempty"`
	Shapes   []state.Learned `json:"shapes"`
}

func buildStatus(srv *proxy.Server, col *watch.Collector, l *learn.Learner, start time.Time) statusReport {
	r := statusReport{Version: lqo.Version, Mode: "shadow", Started: start.UTC(), Active: srv.Active(), Total: srv.Total(), Watch: col.Stats()}
	if l != nil {
		st := l.Stats()
		r.Learning = &st
		r.Shapes = l.Shapes()
	} else {
		for _, s := range col.Snapshot() {
			e := state.Learned{Key: s.Key, ID: s.ID, Database: s.Database, Normalized: s.Normalized, Calls: s.Calls, TotalMs: s.TotalMs, P50Ms: s.P50Ms, P95Ms: s.P95Ms, LastSeen: s.LastSeen, Status: "measured"}
			if len(s.Samples) > 0 {
				e.Example = s.Samples[0].Text
			}
			if s.Skip != "" {
				e.Status, e.Reason = learn.Skipped, s.Skip
			}
			r.Shapes = append(r.Shapes, e)
		}
	}
	if r.Shapes == nil {
		r.Shapes = []state.Learned{}
	}
	return r
}

func statusHandler(srv *proxy.Server, col *watch.Collector, l *learn.Learner, start time.Time) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(buildStatus(srv, col, l, start))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeStatusText(w, buildStatus(srv, col, l, start))
	})
	return mux
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
			fmt.Fprintf(w, "  proven: %q, about %.0f%% faster on the replica\n", s.Proven, s.Gain*100)
			for _, st := range s.Settings {
				fmt.Fprintf(w, "    SET LOCAL %s = %s;\n", st.Name, st.Value)
			}
		case learn.Learning:
			if s.Rounds > 0 {
				fmt.Fprintf(w, "  learning: %d rounds on %d texts\n", s.Rounds, s.Texts)
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
