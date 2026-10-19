// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package report prints an advisor report as text, JSON or Markdown.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/learned-query-optimizer/lqo/internal/advisor"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
)

// Write prints the report in the given format: text, json or markdown.
func Write(w io.Writer, rep *advisor.Report, format string) error {
	switch format {
	case "", "text":
		return Text(w, rep)
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(rep)
	case "markdown", "md":
		return Markdown(w, rep)
	}
	return fmt.Errorf("unknown report format %q (use text, json or markdown)", format)
}

func group(rep *advisor.Report) (faster, keep, other []advisor.ShapeResult) {
	for _, s := range rep.Shapes {
		switch s.Verdict {
		case advisor.Faster:
			faster = append(faster, s)
		case advisor.Keep:
			keep = append(keep, s)
		default:
			other = append(other, s)
		}
	}
	return
}

// Totals estimates the time the tested shapes take per run of the
// workload, with and without the suggested hints. Each shape's time per
// call is the average of its timed samples, multiplied by its calls.
func Totals(rep *advisor.Report) (before, after float64) {
	for _, s := range rep.Shapes {
		if (s.Verdict != advisor.Faster && s.Verdict != advisor.Keep) || len(s.Samples) == 0 {
			continue
		}
		n := float64(len(s.Samples))
		b := s.BaselineMs / n * float64(s.Calls)
		before += b
		if s.Verdict == advisor.Faster {
			after += s.Best.Ms / n * float64(s.Calls)
		} else {
			after += b
		}
	}
	return
}

func ms(v float64) string {
	switch {
	case v >= 1000:
		return addCommas(fmt.Sprintf("%.0f", v)) + " ms"
	case v >= 100:
		return fmt.Sprintf("%.0f ms", v)
	case v >= 10:
		return fmt.Sprintf("%.1f ms", v)
	default:
		return fmt.Sprintf("%.2f ms", v)
	}
}

func addCommas(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}
	var b strings.Builder
	pre := n % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < n; i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func pct(g float64) string { return fmt.Sprintf("−%.0f%%", g*100) }

func calls(n int) string {
	if n == 1 {
		return "1 call"
	}
	return fmt.Sprintf("%d calls", n)
}

func serverLine(rep *advisor.Report) string {
	name := rep.Server
	if i := strings.Index(name, " on "); i > 0 {
		name = name[:i]
	}
	role := "standby"
	if !rep.Standby {
		role = "primary, allowed with --allow-primary"
	}
	return fmt.Sprintf("%s, database %s (%s)", name, rep.Database, role)
}

func settingsLine(rep *advisor.Report) string {
	s := fmt.Sprintf("%s hints (%d), %d timed runs after a warm-up, up to %d samples per shape, a hint must save %.0f%%",
		rep.HintSet, rep.Arms, rep.Repeat, rep.SamplesPer, rep.MinGain*100)
	switch {
	case strings.HasPrefix(rep.JoinHints, "pg_hint_plan"):
		s += "; row-count hints with " + rep.JoinHints
	case rep.JoinHints != "":
		s += "; row-count hints " + rep.JoinHints
	}
	return s
}

// Text prints the report for a terminal.
func Text(w io.Writer, rep *advisor.Report) error {
	faster, keep, other := group(rep)
	p := func(format string, a ...any) { fmt.Fprintf(w, format, a...) }
	p("LQO advisor report\n\n")
	p("Server     %s\n", serverLine(rep))
	p("Workload   %s: %d statements, %d shapes\n", strings.Join(rep.Workload, ", "), rep.Statements, rep.ShapesTotal)
	p("Settings   %s\n", settingsLine(rep))
	if rep.OtherDatabases > 0 {
		p("           %d logged statements from other databases were left out\n", rep.OtherDatabases)
	}
	if len(rep.DroppedArms) > 0 {
		p("           not on this server: %s\n", strings.Join(rep.DroppedArms, ", "))
	}
	if rep.Interrupted {
		p("\nThe run was interrupted; the report covers the shapes finished before that.\n")
	}

	p("\nFaster with a hint: %d of %d tested\n", len(faster), len(faster)+len(keep))
	for i, s := range faster {
		p("\n  %d. %s  %s  (%s)\n", i+1, s.ID[:8], sqltext.OneLine(s.Example, 64), calls(s.Calls))
		p("     %s → %s (%s) with %q\n", ms(s.BaselineMs/float64(len(s.Samples))), ms(s.Best.Ms/float64(len(s.Samples))), pct(s.Best.Gain), s.Best.Name)
		p("     Plan: %s\n", s.Best.Change)
		for _, l := range s.Best.Lines {
			p("       %s\n", l)
		}
		for _, n := range s.Notes {
			p("     Note: %s\n", n)
		}
	}
	if len(keep) > 0 {
		p("\nKeep the default plan: %d\n\n", len(keep))
		for _, s := range keep {
			p("  %s  %-48s %10s  %s\n", s.ID[:8], sqltext.OneLine(s.Example, 48), ms(s.BaselineMs/float64(len(s.Samples))), s.Reason)
		}
	}
	if len(other) > 0 {
		p("\nNot tested: %d\n\n", len(other))
		for _, s := range other {
			p("  %s  %-48s %s: %s\n", s.ID[:8], sqltext.OneLine(s.Example, 48), s.Verdict, s.Reason)
		}
	}
	if before, after := Totals(rep); before > 0 {
		p("\nEstimated time for one pass of the tested statements: %s now, %s with these hints (%s).\n",
			ms(before), ms(after), pct(1-after/before))
	}
	p("Times are planning plus execution, as EXPLAIN ANALYZE reports them, median of %d runs per plan.\n", rep.Repeat)
	return nil
}

// Markdown prints the report for a pull request, an issue or a wiki page.
func Markdown(w io.Writer, rep *advisor.Report) error {
	faster, keep, other := group(rep)
	p := func(format string, a ...any) { fmt.Fprintf(w, format, a...) }
	p("# LQO advisor report\n\n")
	p("- **Server:** %s\n", serverLine(rep))
	p("- **Workload:** %s: %d statements, %d shapes\n", strings.Join(rep.Workload, ", "), rep.Statements, rep.ShapesTotal)
	p("- **Settings:** %s\n", settingsLine(rep))
	if rep.Interrupted {
		p("- **Interrupted:** the report covers the shapes finished before that\n")
	}
	if before, after := Totals(rep); before > 0 {
		p("- **Estimate:** one pass of the tested statements takes %s now, %s with these hints (%s)\n", ms(before), ms(after), pct(1-after/before))
	}
	p("\n## Faster with a hint (%d of %d tested)\n\n", len(faster), len(faster)+len(keep))
	if len(faster) == 0 {
		p("None.\n")
	}
	for _, s := range faster {
		n := float64(len(s.Samples))
		p("### `%s` %s → %s (%s)\n\n", s.ID[:8], ms(s.BaselineMs/n), ms(s.Best.Ms/n), pct(s.Best.Gain))
		p("```sql\n%s\n```\n\n", strings.TrimSpace(s.Example))
		p("- **Hint:** %s, %s\n", s.Best.Name, calls(s.Calls))
		p("- **Plan:** %s\n", s.Best.Change)
		for _, note := range s.Notes {
			p("- **Note:** %s\n", note)
		}
		p("\n```sql\n%s\n```\n\n", strings.Join(s.Best.Lines, "\n"))
	}
	if len(keep) > 0 {
		p("## Keep the default plan (%d)\n\n| Shape | Query | Time | Why |\n|---|---|---|---|\n", len(keep))
		for _, s := range keep {
			p("| `%s` | %s | %s | %s |\n", s.ID[:8], mdCell(sqltext.OneLine(s.Example, 60)), ms(s.BaselineMs/float64(len(s.Samples))), mdCell(s.Reason))
		}
		p("\n")
	}
	if len(other) > 0 {
		p("## Not tested (%d)\n\n| Shape | Query | Why |\n|---|---|---|\n", len(other))
		for _, s := range other {
			p("| `%s` | %s | %s: %s |\n", s.ID[:8], mdCell(sqltext.OneLine(s.Example, 60)), s.Verdict, mdCell(s.Reason))
		}
		p("\n")
	}
	p("Times are planning plus execution, as EXPLAIN ANALYZE reports them, median of %d runs per plan.\n", rep.Repeat)
	return nil
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "`", "'")
}
