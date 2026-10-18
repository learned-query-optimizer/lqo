// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package workload reads the queries LQO should look at, from a SQL file or
// from a PostgreSQL server log, and groups them by shape.
package workload

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/learned-query-optimizer/lqo/internal/sqltext"
)

// Statement is one query as the workload recorded it.
type Statement struct {
	Text     string
	Source   string  // file:line
	Database string  // from the log, when it says
	LoggedMs float64 // how long the server took, when the log says; -1 if unknown
	QueryID  string  // the server's query ID, when the log has one
}

// Formats the readers understand.
const (
	FormatAuto   = "auto"
	FormatSQL    = "sql"
	FormatStderr = "stderr"
	FormatCSV    = "csv"
	FormatJSON   = "json"
)

// ReadFile reads a workload file. With format auto, the format is guessed
// from the file name, then from the first lines.
func ReadFile(path, format string) ([]Statement, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if format == "" || format == FormatAuto {
		format = Detect(path, data)
	}
	name := filepath.Base(path)
	var st []Statement
	switch format {
	case FormatSQL:
		st = ReadSQL(name, string(data))
	case FormatStderr:
		st, err = ReadStderrLog(name, bytes.NewReader(data))
	case FormatCSV:
		st, err = ReadCSVLog(name, bytes.NewReader(data))
	case FormatJSON:
		st, err = ReadJSONLog(name, bytes.NewReader(data))
	default:
		return nil, "", fmt.Errorf("unknown workload format %q (use sql, stderr, csv or json)", format)
	}
	return st, format, err
}

// Detect guesses a workload file's format.
func Detect(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sql":
		return FormatSQL
	case ".csv":
		return FormatCSV
	case ".json", ".jsonl":
		return FormatJSON
	}
	head := data
	if len(head) > 64*1024 {
		head = head[:64*1024]
	}
	trimmed := bytes.TrimSpace(head)
	if bytes.HasPrefix(trimmed, []byte("{")) {
		return FormatJSON
	}
	if csvLine.Match(head) {
		return FormatCSV
	}
	if severityRe.Match(head) {
		return FormatStderr
	}
	return FormatSQL
}

var csvLine = regexp.MustCompile(`(?m)^\d{4}-\d\d-\d\d [0-9:.]+ [A-Za-z+0-9:-]*,[^\n]*,"?(LOG|ERROR|WARNING|NOTICE|FATAL|STATEMENT)"?,`)

// ReadSQL reads a script of statements separated by semicolons.
func ReadSQL(name, script string) []Statement {
	var out []Statement
	for _, p := range sqltext.SplitLines(script) {
		out = append(out, Statement{Text: p.Text, Source: fmt.Sprintf("%s:%d", name, p.Line), LoggedMs: -1})
	}
	return out
}

// severityRe finds where a log message starts in a stderr log line, after
// whatever log_line_prefix put in front of it.
var severityRe = regexp.MustCompile(`(?m)(?:^|[\s\]:])(LOG|ERROR|WARNING|NOTICE|INFO|FATAL|PANIC|DETAIL|HINT|STATEMENT|CONTEXT|QUERY|DEBUG[1-5]?):  `)

// message is one log message: severity, text, and the DETAIL that follows.
type message struct {
	severity string
	text     string
	detail   string
	database string
	queryID  string
	line     int
}

// ReadStderrLog reads PostgreSQL's plain text log. Lines that start with a
// tab continue the message before them, as the server writes them.
func ReadStderrLog(name string, r io.Reader) ([]Statement, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	var msgs []*message
	var cur *message // the message that continuation lines belong to
	inDetail := false
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSuffix(sc.Text(), "\r")
		if strings.HasPrefix(line, "\t") && cur != nil {
			if inDetail {
				cur.detail += "\n" + line[1:]
			} else {
				cur.text += "\n" + line[1:]
			}
			continue
		}
		loc := severityRe.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}
		sev := line[loc[2]:loc[3]]
		text := line[loc[1]:]
		if sev == "DETAIL" {
			if cur != nil {
				cur.detail = text
				inDetail = true
			}
			continue
		}
		cur = &message{severity: sev, text: text, line: lineNo}
		inDetail = false
		msgs = append(msgs, cur)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	var out []Statement
	for _, m := range msgs {
		if m.severity != "LOG" {
			continue
		}
		if st, ok := fromMessage(name, m); ok {
			out = append(out, st)
		}
	}
	return out, nil
}

var (
	durationRe  = regexp.MustCompile(`^duration: ([0-9.]+) ms\s*`)
	executeRe   = regexp.MustCompile(`^execute [^:]*: `)
	paramItemRe = regexp.MustCompile(`\$(\d+) = (NULL|'(?:[^']|'')*')`)
)

// fromMessage turns one LOG message into a statement, if it holds one.
func fromMessage(name string, m *message) (Statement, bool) {
	text := m.text
	ms := -1.0
	if d := durationRe.FindStringSubmatch(text); d != nil {
		ms, _ = strconv.ParseFloat(d[1], 64)
		text = text[len(d[0]):]
	}
	st := Statement{Source: fmt.Sprintf("%s:%d", name, m.line), LoggedMs: ms, Database: m.database, QueryID: m.queryID}
	switch {
	case strings.HasPrefix(text, "statement: "):
		st.Text = strings.TrimSpace(strings.TrimPrefix(text, "statement: "))
	case executeRe.MatchString(text):
		st.Text = strings.TrimSpace(text[len(executeRe.FindString(text)):])
		if p := parseParams(m.detail); len(p) > 0 {
			st.Text = sqltext.Substitute(st.Text, p)
		}
	case strings.HasPrefix(text, "plan:"):
		q, params := autoExplainQuery(strings.TrimPrefix(text, "plan:"))
		if q == "" {
			return st, false
		}
		st.Text = q
		if len(params) > 0 {
			st.Text = sqltext.Substitute(st.Text, params)
		}
	default:
		return st, false
	}
	st.Text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(st.Text), ";"))
	if st.Text == "" {
		return st, false
	}
	return st, true
}

// parseParams reads "parameters: $1 = '5', $2 = NULL" from a DETAIL line.
func parseParams(detail string) map[int]string {
	i := strings.Index(strings.ToLower(detail), "parameters: ")
	if i < 0 {
		return nil
	}
	out := map[int]string{}
	for _, m := range paramItemRe.FindAllStringSubmatch(detail[i:], -1) {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			out[n] = m[2]
		}
	}
	return out
}

var planLineRe = regexp.MustCompile(`\(cost=[0-9.]+\.\.[0-9.]+ rows=|\(actual (time|rows)=`)

// autoExplainQuery pulls the query text out of an auto_explain message, in
// its text or JSON format.
func autoExplainQuery(body string) (string, map[int]string) {
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[") {
		var doc map[string]any
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			var arr []map[string]any
			if json.Unmarshal([]byte(body), &arr) != nil || len(arr) == 0 {
				return "", nil
			}
			doc = arr[0]
		}
		q, _ := doc["Query Text"].(string)
		var params map[int]string
		if p, ok := doc["Query Parameters"].(string); ok {
			params = parseParams("parameters: " + p)
		}
		return strings.TrimSpace(q), params
	}
	lines := strings.Split(body, "\n")
	var q []string
	var params map[int]string
	in := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "Query Text: ") {
			in = true
			q = append(q, strings.TrimPrefix(t, "Query Text: "))
			continue
		}
		if strings.HasPrefix(t, "Query Parameters: ") {
			params = parseParams("parameters: " + strings.TrimPrefix(t, "Query Parameters: "))
			in = false
			continue
		}
		if !in {
			continue
		}
		if planLineRe.MatchString(l) {
			break
		}
		q = append(q, l)
	}
	return strings.TrimSpace(strings.Join(q, "\n")), params
}

// csvlog columns, from the PostgreSQL documentation. Later versions add
// columns at the end, so these positions hold in every version.
const (
	csvDatabase = 2
	csvSeverity = 11
	csvMessage  = 13
	csvDetail   = 14
	csvQueryID  = 25 // PostgreSQL 14 and later
)

// ReadCSVLog reads PostgreSQL's csvlog format.
func ReadCSVLog(name string, r io.Reader) ([]Statement, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.ReuseRecord = false
	var out []Statement
	row := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		row++
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			return nil, err
		}
		if len(rec) <= csvDetail || rec[csvSeverity] != "LOG" {
			continue
		}
		line, _ := cr.FieldPos(0)
		m := &message{severity: "LOG", text: rec[csvMessage], detail: rec[csvDetail], database: rec[csvDatabase], line: line}
		if len(rec) > csvQueryID && rec[csvQueryID] != "0" {
			m.queryID = rec[csvQueryID]
		}
		if st, ok := fromMessage(name, m); ok {
			out = append(out, st)
		}
	}
	return out, nil
}

// ReadJSONLog reads PostgreSQL's jsonlog format (PostgreSQL 15 and later):
// one JSON object per line.
func ReadJSONLog(name string, r io.Reader) ([]Statement, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	var out []Statement
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec struct {
			Severity string          `json:"error_severity"`
			Message  string          `json:"message"`
			Detail   string          `json:"detail"`
			DBName   string          `json:"dbname"`
			QueryID  json.RawMessage `json:"query_id"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, lineNo, err)
		}
		if rec.Severity != "LOG" {
			continue
		}
		m := &message{severity: "LOG", text: rec.Message, detail: rec.Detail, database: rec.DBName, line: lineNo}
		if q := strings.Trim(string(rec.QueryID), `"`); q != "" && q != "0" && q != "null" {
			m.queryID = q
		}
		if st, ok := fromMessage(name, m); ok {
			out = append(out, st)
		}
	}
	return out, sc.Err()
}

// Shape is a group of statements that differ only in their values.
type Shape struct {
	ID         string // the fingerprint
	Normalized string
	Calls      int
	LoggedMs   float64 // total logged time, 0 when the log had none
	HasTimes   bool
	Samples    []Sample // distinct texts, slowest and most frequent first
	QueryIDs   []string
	Skip       string // why LQO won't run this shape, if it won't
	Sources    []string
}

// Sample is one distinct text of a shape.
type Sample struct {
	Text     string
	Calls    int
	MaxMs    float64
	Source   string
	Database string
}

// Group collects statements into shapes, ordered by total logged time,
// then by number of calls, then by first appearance.
func Group(statements []Statement) []*Shape {
	byID := map[string]*Shape{}
	var order []*Shape
	sampleIdx := map[string]map[string]int{}
	for _, st := range statements {
		id := sqltext.Fingerprint(st.Text)
		sh := byID[id]
		if sh == nil {
			sh = &Shape{ID: id, Normalized: sqltext.Normalize(st.Text)}
			byID[id] = sh
			order = append(order, sh)
			sampleIdx[id] = map[string]int{}
		}
		sh.Calls++
		if st.LoggedMs >= 0 {
			sh.LoggedMs += st.LoggedMs
			sh.HasTimes = true
		}
		if st.QueryID != "" && !contains(sh.QueryIDs, st.QueryID) {
			sh.QueryIDs = append(sh.QueryIDs, st.QueryID)
		}
		key := sqltext.TrimLeadingComments(st.Text)
		if i, ok := sampleIdx[id][key]; ok {
			sh.Samples[i].Calls++
			if st.LoggedMs > sh.Samples[i].MaxMs {
				sh.Samples[i].MaxMs = st.LoggedMs
			}
		} else {
			sampleIdx[id][key] = len(sh.Samples)
			sh.Samples = append(sh.Samples, Sample{Text: key, Calls: 1, MaxMs: st.LoggedMs, Source: st.Source, Database: st.Database})
			if len(sh.Sources) < 3 {
				sh.Sources = append(sh.Sources, st.Source)
			}
		}
	}
	for _, sh := range order {
		sort.SliceStable(sh.Samples, func(i, j int) bool {
			a, b := sh.Samples[i], sh.Samples[j]
			if a.MaxMs != b.MaxMs {
				return a.MaxMs > b.MaxMs
			}
			return a.Calls > b.Calls
		})
		if r := sqltext.Classify(sh.Samples[0].Text); r != "" {
			sh.Skip = r
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.LoggedMs != b.LoggedMs {
			return a.LoggedMs > b.LoggedMs
		}
		return a.Calls > b.Calls
	})
	return order
}

// FilterDatabase drops statements the log says ran in another database.
// Statements with no database recorded are kept.
func FilterDatabase(statements []Statement, db string) (kept []Statement, dropped int) {
	for _, st := range statements {
		if st.Database != "" && st.Database != db {
			dropped++
			continue
		}
		kept = append(kept, st)
	}
	return kept, dropped
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
