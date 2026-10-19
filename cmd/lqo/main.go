// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Command lqo finds the PostgreSQL queries whose plans a planner setting
// makes clearly faster, by trying them on a replica or a copy.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/learned-query-optimizer/lqo"
	"github.com/learned-query-optimizer/lqo/internal/advisor"
	"github.com/learned-query-optimizer/lqo/internal/demo"
	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/report"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/state"
	"github.com/learned-query-optimizer/lqo/internal/workload"
)

const usage = `lqo finds the PostgreSQL queries a planner setting makes clearly faster.

Usage:
  lqo advise [flags] WORKLOAD...   try a workload's queries with each hint and report
  lqo serve [flags]                run as a proxy, measure live queries, learn on a replica
  lqo switch off|on [SHAPE]        turn a running lqo serve's hints off or on
  lqo show [flags]                 print what the state file holds
  lqo report [flags] REPORT.json   print a saved JSON report as text or Markdown
  lqo hints [--hints full]         list the hints lqo tries
  lqo demo [flags]                 build the lqo_demo schema and its workload file
  lqo version                      print the version

A workload is a .sql file of statements, or a PostgreSQL server log
(stderr, csvlog or jsonlog) with log_min_duration_statement or auto_explain.

Run "lqo advise -h" or "lqo serve -h" for their flags. The connection string comes from
--dsn, then the LQO_DSN variable, then the usual PG* variables.
Documentation: https://lqo.net
`

// Exit codes.
const (
	exitOK      = 0
	exitError   = 1
	exitRefused = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitRefused
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch args[0] {
	case "advise":
		err = cmdAdvise(ctx, args[1:], stdout, stderr)
	case "serve":
		err = cmdServe(ctx, args[1:], stdout, stderr)
	case "switch":
		err = cmdSwitch(args[1:], stdout, stderr)
	case "show":
		err = cmdShow(args[1:], stdout, stderr)
	case "report":
		err = cmdReport(args[1:], stdout, stderr)
	case "hints":
		err = cmdHints(args[1:], stdout, stderr)
	case "demo":
		err = cmdDemo(ctx, args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "lqo %s (%s, %s/%s)\n", lqo.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "lqo: unknown command %q\n\n%s", args[0], usage)
		return exitRefused
	}
	if err == nil {
		return exitOK
	}
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	var ue usageError
	if errors.As(err, &ue) || errors.Is(err, advisor.ErrPrimary) {
		fmt.Fprintf(stderr, "lqo: %v\n", err)
		return exitRefused
	}
	fmt.Fprintf(stderr, "lqo: %v\n", err)
	return exitError
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// parse reads flags that may come before, after or between positional
// arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usageError{err.Error()}
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		if rest[0] == "--" {
			return append(pos, rest[1:]...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

type settingList []hints.Setting

func (s *settingList) String() string { return "" }
func (s *settingList) Set(v string) error {
	name, val, ok := strings.Cut(v, "=")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return fmt.Errorf("want name=value, got %q", v)
	}
	*s = append(*s, hints.Setting{Name: name, Value: strings.TrimSpace(val)})
	return nil
}

func dsnFrom(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("LQO_DSN")
}

func cmdAdvise(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lqo advise", flag.ContinueOnError)
	fs.SetOutput(stderr)
	d := advisor.Defaults
	dsn := fs.String("dsn", "", "connection string, such as postgres://user@replica:5432/shop")
	format := fs.String("format", "auto", "workload format: auto, sql, stderr, csv or json")
	hintSet := fs.String("hints", d.HintSet, "hint set: standard (13 hints) or full (48)")
	repeat := fs.Int("repeat", d.Repeat, "timed runs per plan, after one warm-up run")
	samples := fs.Int("samples", d.Samples, "distinct texts tried per query shape")
	top := fs.Int("top", d.Top, "query shapes tried, slowest first when the log has times")
	minGain := fs.Float64("min-gain", d.MinGain*100, "percent of the time a hint must save")
	factor := fs.Float64("timeout-factor", d.TimeoutFactor, "stop a hinted run at this multiple of the default plan's time")
	maxTime := fs.Duration("max-time", d.MaxTime, "stop the default plan's runs after this long")
	allowPrimary := fs.Bool("allow-primary", false, "run on a primary server, not only a standby")
	allDBs := fs.Bool("all-databases", false, "keep logged statements from every database, not only the one connected to")
	statePath := fs.String("state", "lqo-state.json", "state file to record findings in")
	noState := fs.Bool("no-state", false, "don't write the state file")
	reportFmt := fs.String("report", "text", "report format: text, json or markdown")
	out := fs.String("out", "", "write the report to this file instead of standard output")
	quiet := fs.Bool("quiet", false, "don't print progress")
	var session settingList
	fs.Var(&session, "set", "session setting for every trial, such as search_path=app (may repeat)")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: lqo advise [flags] WORKLOAD...\n\nTries each query shape in the workload under the default plan and under\neach hint, on a standby (or, with --allow-primary, a copy), and reports the\nhints that are clearly and consistently faster. It changes nothing on the\nserver: every trial is an EXPLAIN ANALYZE in a READ ONLY transaction that\nis rolled back.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	files, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fs.Usage()
		return usageError{"give at least one workload file"}
	}
	switch *reportFmt {
	case "text", "json", "markdown", "md":
	default:
		return usageError{fmt.Sprintf("unknown report format %q", *reportFmt)}
	}
	if *minGain <= 0 || *minGain >= 100 {
		return usageError{"--min-gain is a percent between 0 and 100"}
	}
	if _, err := hints.Set(*hintSet); err != nil {
		return usageError{err.Error()}
	}
	switch *format {
	case "auto", "sql", "stderr", "csv", "json":
	default:
		return usageError{fmt.Sprintf("unknown workload format %q", *format)}
	}

	var statements []workload.Statement
	var names []string
	for _, f := range files {
		st, detected, err := workload.ReadFile(f, *format)
		if err != nil {
			return err
		}
		statements = append(statements, st...)
		names = append(names, fmt.Sprintf("%s (%s)", filepath.Base(f), detected))
	}

	cfg, err := pgwire.ParseConfig(dsnFrom(*dsn))
	if err != nil {
		return usageError{err.Error()}
	}
	progress := stderr
	if *quiet {
		progress = io.Discard
	}
	conn, err := pgwire.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	rep := &advisor.Report{LQOVersion: lqo.Version, Started: time.Now().UTC(), Target: cfg.Describe(), Workload: names}
	opt := advisor.Options{
		HintSet:       *hintSet,
		Repeat:        *repeat,
		Samples:       *samples,
		Top:           *top,
		MinGain:       *minGain / 100,
		TimeoutFactor: *factor,
		MaxTime:       *maxTime,
		AllowPrimary:  *allowPrimary,
		Session:       session,
		Progress:      progress,
	}
	adv, err := advisor.New(ctx, conn, opt, rep)
	if err != nil {
		return err
	}
	if !rep.Standby {
		fmt.Fprintln(progress, "Note: this is a primary server. Every trial is read-only and rolled back, but the queries still run, several times each.")
	}
	if !*allDBs {
		var dropped int
		statements, dropped = workload.FilterDatabase(statements, rep.Database)
		rep.OtherDatabases = dropped
	}
	rep.Statements = len(statements)
	shapes := workload.Group(statements)
	if len(shapes) == 0 {
		return fmt.Errorf("no statements found in %s", strings.Join(files, ", "))
	}
	fmt.Fprintf(progress, "%d statements, %d shapes, %d hints\n", len(statements), len(shapes), rep.Arms)

	runErr := adv.Run(ctx, shapes, rep)
	rep.Finished = time.Now().UTC()

	if !*noState && len(rep.Shapes) > 0 {
		if err := saveState(*statePath, rep, progress); err != nil {
			fmt.Fprintf(stderr, "lqo: couldn't save the state file: %v\n", err)
		}
	}
	if err := writeReport(*out, rep, *reportFmt, stdout); err != nil {
		return err
	}
	if runErr != nil {
		return runErr
	}
	if rep.Interrupted {
		return errors.New("interrupted")
	}
	return nil
}

func writeReport(path string, rep *advisor.Report, format string, stdout io.Writer) error {
	if path == "" {
		return report.Write(stdout, rep, format)
	}
	var b strings.Builder
	if err := report.Write(&b, rep, format); err != nil {
		return err
	}
	return state.WriteAtomic(path, []byte(b.String()))
}

func saveState(path string, rep *advisor.Report, progress io.Writer) error {
	f, note, err := state.LoadOrSetAside(path)
	if err != nil {
		return err
	}
	if note != "" {
		fmt.Fprintln(progress, "Note: "+note)
	}
	for _, s := range rep.Shapes {
		if s.Verdict != advisor.Faster && s.Verdict != advisor.Keep {
			continue
		}
		e := &state.Entry{
			ID:         s.ID,
			QueryID:    s.QueryID,
			Normalized: s.Normalized,
			Example:    s.Example,
			Database:   rep.Database,
			Server:     rep.ServerNum,
			Tested:     rep.Finished,
			Verdict:    s.Verdict,
			Reason:     s.Reason,
			BaselineMs: s.BaselineMs / float64(max(1, len(s.Samples))),
		}
		if s.Best != nil {
			e.Hint = s.Best.Name
			e.Settings = s.Best.Settings
			e.HintMs = s.Best.Ms / float64(max(1, len(s.Samples)))
			e.Gain = s.Best.Gain
			e.Change = s.Best.Change
		}
		f.Put(e)
	}
	return f.Save(path)
}

func cmdShow(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lqo show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	statePath := fs.String("state", "lqo-state.json", "state file to read")
	asJSON := fs.Bool("json", false, "print the file as JSON")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	f, err := state.Load(*statePath)
	if err != nil {
		return err
	}
	if *asJSON {
		b, err := os.ReadFile(*statePath)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stdout, "{}")
			return nil
		}
		if err != nil {
			return err
		}
		_, err = stdout.Write(b)
		return err
	}
	if len(f.Shapes) == 0 && len(f.Serve) == 0 {
		fmt.Fprintf(stdout, "%s holds nothing yet. Run lqo advise or lqo serve first.\n", *statePath)
		return nil
	}
	if len(f.Serve) > 0 {
		showServe(stdout, *statePath, f)
		if len(f.Shapes) == 0 {
			return nil
		}
		fmt.Fprintln(stdout)
	}
	fmt.Fprintf(stdout, "%s, updated %s: %d shapes from lqo advise\n", *statePath, f.Updated.Format("2006-01-02 15:04 UTC"), len(f.Shapes))
	for _, e := range f.Sorted() {
		fmt.Fprintf(stdout, "\n%s  %s\n", e.ID[:8], sqltext.OneLine(e.Example, 70))
		fmt.Fprintf(stdout, "  database %s, tested %s\n", e.Database, e.Tested.Format("2006-01-02 15:04 UTC"))
		if e.Verdict == advisor.Faster {
			fmt.Fprintf(stdout, "  faster with %q: %.1f ms → %.1f ms (−%.0f%%); %s\n", e.Hint, e.BaselineMs, e.HintMs, e.Gain*100, e.Change)
			for _, s := range e.Settings {
				fmt.Fprintf(stdout, "    SET LOCAL %s = %s;\n", s.Name, s.Value)
			}
		} else {
			fmt.Fprintf(stdout, "  keep the default plan (%.1f ms): %s\n", e.BaselineMs, e.Reason)
		}
	}
	return nil
}

func cmdReport(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lqo report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("report", "text", "format: text, json or markdown")
	files, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(files) != 1 {
		return usageError{"give one JSON report, as written by lqo advise --report json"}
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		return err
	}
	var rep advisor.Report
	if err := json.Unmarshal(b, &rep); err != nil {
		return fmt.Errorf("%s isn't a JSON report: %w", files[0], err)
	}
	return report.Write(stdout, &rep, *format)
}

func cmdHints(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lqo hints", flag.ContinueOnError)
	fs.SetOutput(stderr)
	set := fs.String("hints", "standard", "hint set: standard or full")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	arms, err := hints.Set(*set)
	if err != nil {
		return usageError{err.Error()}
	}
	for _, a := range arms {
		fmt.Fprintf(stdout, "%-36s %s\n", a.Name, strings.Join(a.SetLocal(), " "))
	}
	return nil
}

func cmdDemo(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lqo demo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dsn := fs.String("dsn", "", "connection string for a test database")
	scale := fs.Float64("scale", 1, "data size: 1 is 200,000 customers, 1 million orders, 3 million order lines")
	wl := fs.String("workload", "lqo-demo.sql", "where to write the demo workload")
	only := fs.Bool("workload-only", false, "write the workload file and don't touch the database")
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: lqo demo [flags]\n\nBuilds the lqo_demo schema (dropping any earlier one) with shop data that\nfools the planner's row estimates, and writes a workload file to try LQO on.\nUse a test database: it creates tables and writes about 600 MB at scale 1.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if *scale <= 0 || *scale > 100 {
		return usageError{"--scale is between 0 and 100"}
	}
	if err := state.WriteAtomic(*wl, []byte(demo.Workload)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote the workload to %s\n", *wl)
	if *only {
		return nil
	}
	cfg, err := pgwire.ParseConfig(dsnFrom(*dsn))
	if err != nil {
		return usageError{err.Error()}
	}
	conn, err := pgwire.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	start := time.Now()
	err = demo.Setup(ctx, conn, *scale, func(s string) { fmt.Fprintln(stderr, "  "+s) })
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Built the %s schema in %s.\n\nNext, with the same connection string:\n  lqo advise --allow-primary --dsn ... %s\n",
		demo.Schema, time.Since(start).Round(time.Second), *wl)
	return nil
}
