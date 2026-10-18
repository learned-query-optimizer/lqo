// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package sqltext

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Normalize turns a statement into its shape: comments and spacing go,
// keywords are lower-cased, and every literal value and $n parameter
// becomes ?, so calls that differ only in their values share one shape.
// A list of values inside IN (...) or ARRAY[...] becomes a single ?...,
// however long it was.
func Normalize(sql string) string {
	code := Code(sql)
	parts := make([]string, 0, len(code))
	for i, t := range code {
		switch t.Kind {
		case String, Number, Param:
			// A sign in front of a number is part of the value.
			if len(parts) > 0 && (parts[len(parts)-1] == "-" || parts[len(parts)-1] == "+") && t.Kind == Number && signIsUnary(code, i-1) {
				parts = parts[:len(parts)-1]
			}
			parts = append(parts, "?")
		case Word:
			parts = append(parts, strings.ToLower(t.Text))
		default:
			parts = append(parts, t.Text)
		}
	}
	return collapseLists(parts)
}

// signIsUnary reports whether the + or - at i starts a value rather than
// subtracting from what comes before it.
func signIsUnary(code []Token, i int) bool {
	if i <= 0 {
		return true
	}
	p := code[i-1]
	switch p.Kind {
	case Op:
		return p.Text != ")" && p.Text != "]"
	case Word:
		switch strings.ToLower(p.Text) {
		case "select", "where", "and", "or", "not", "when", "then", "else", "in", "values",
			"limit", "offset", "by", "between", "like", "ilike", "is", "return", "on", "having", "case":
			return true
		}
	}
	return false
}

// collapseLists replaces "( ? , ? , ? )" after IN, and "[ ? , ? ]" after
// ARRAY, with a single ?... so lists of any length share a shape.
func collapseLists(parts []string) string {
	var out []string
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		out = append(out, p)
		var open, close string
		if (p == "in" || p == "any" || p == "all") && i+1 < len(parts) && parts[i+1] == "(" {
			open, close = "(", ")"
		} else if p == "array" && i+1 < len(parts) && parts[i+1] == "[" {
			open, close = "[", "]"
		} else {
			continue
		}
		j := i + 2
		onlyValues := true
		count := 0
		for j < len(parts) && parts[j] != close {
			if parts[j] == "?" {
				count++
			} else if parts[j] != "," {
				onlyValues = false
				break
			}
			j++
		}
		if onlyValues && j < len(parts) && count > 0 {
			out = append(out, open, "?...", close)
			i = j
		}
	}
	return strings.Join(out, " ")
}

// Fingerprint is a stable ID for a statement's shape: the first 16 hex
// digits of the SHA-256 of its normalised text.
func Fingerprint(sql string) string {
	sum := sha256.Sum256([]byte(Normalize(sql)))
	return hex.EncodeToString(sum[:8])
}

// OneLine is the statement on one line with single spaces, cut to max
// characters with an ellipsis. Values are kept.
func OneLine(sql string, max int) string {
	var b strings.Builder
	for _, t := range Lex(sql) {
		switch t.Kind {
		case Comment:
			continue
		case Space:
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
		default:
			b.WriteString(t.Text)
		}
	}
	s := strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
	if max > 0 && len([]rune(s)) > max {
		r := []rune(s)
		s = string(r[:max-1]) + "…"
	}
	return s
}

// Substitute replaces $n parameters with the given literal SQL text, such
// as '42' or NULL, as written in a server log's "parameters:" line.
func Substitute(sql string, params map[int]string) string {
	var b strings.Builder
	for _, t := range Lex(sql) {
		if t.Kind == Param {
			n, err := strconv.Atoi(t.Text[1:])
			if v, ok := params[n]; err == nil && ok {
				b.WriteString(v)
				continue
			}
		}
		b.WriteString(t.Text)
	}
	return b.String()
}

// starts lists the first words of statements LQO will run.
var starts = map[string]bool{"select": true, "with": true, "values": true, "table": true}

// changes are words that, anywhere in a statement, mean it writes or
// changes something. Some are also valid column names; a statement that
// uses one unquoted is skipped, which costs a missed suggestion, never a
// write.
var changes = map[string]bool{
	"insert": true, "update": true, "delete": true, "merge": true, "truncate": true, "copy": true,
}

// denied are functions that act outside the query: they stop sessions,
// sleep, change settings, take session locks, reach other servers or write
// large objects. A READ ONLY transaction doesn't stop all of them, so LQO
// won't run a statement that calls one.
var denied = map[string]bool{
	"pg_terminate_backend": true, "pg_cancel_backend": true, "pg_reload_conf": true,
	"pg_rotate_logfile": true, "pg_sleep": true, "pg_sleep_for": true, "pg_sleep_until": true,
	"set_config": true, "pg_notify": true, "pg_logical_emit_message": true, "pg_switch_wal": true,
	"pg_create_restore_point": true, "pg_promote": true, "pg_log_backend_memory_contexts": true,
	"pg_stat_reset": true, "pg_stat_reset_shared": true, "pg_stat_reset_single_table_counters": true,
	"pg_stat_reset_single_function_counters": true, "pg_stat_reset_slru": true,
	"pg_stat_statements_reset": true, "pg_wal_replay_pause": true, "pg_wal_replay_resume": true,
	"pg_advisory_lock": true, "pg_advisory_lock_shared": true, "pg_try_advisory_lock": true,
	"pg_try_advisory_lock_shared": true, "pg_advisory_xact_lock": true, "pg_advisory_xact_lock_shared": true,
	"pg_try_advisory_xact_lock": true, "pg_try_advisory_xact_lock_shared": true,
	"pg_advisory_unlock": true, "pg_advisory_unlock_all": true, "pg_advisory_unlock_shared": true,
	"nextval": true, "setval": true, "txid_current": true, "pg_current_xact_id": true,
	"pg_export_snapshot": true, "pg_create_logical_replication_slot": true,
	"pg_create_physical_replication_slot": true, "pg_drop_replication_slot": true,
	"pg_file_write": true, "pg_file_unlink": true, "pg_file_rename": true,
}

// Classify decides whether LQO may run a statement. It returns "" when the
// statement is a plain read, or the reason it will be skipped.
func Classify(sql string) string {
	code := Code(sql)
	if len(code) == 0 {
		return "empty statement"
	}
	for _, t := range code {
		if t.Kind == Op && t.Text == ";" {
			return "more than one statement"
		}
	}
	first := code[0]
	for first.Kind == Op && first.Text == "(" && len(code) > 1 {
		code = code[1:]
		first = code[0]
	}
	if first.Kind != Word || !starts[strings.ToLower(first.Text)] {
		w := strings.ToUpper(first.Text)
		if w == "EXPLAIN" {
			return "it's already an EXPLAIN"
		}
		return fmt.Sprintf("only reads are tested, and this is %s", w)
	}
	for i, t := range code {
		switch t.Kind {
		case Param:
			return fmt.Sprintf("it has a parameter with no value (%s)", t.Text)
		case Word:
			w := strings.ToLower(t.Text)
			if changes[w] && !(w == "update" && i > 0 && strings.EqualFold(code[i-1].Text, "for")) {
				return fmt.Sprintf("it contains %s, so it may write", strings.ToUpper(w))
			}
			if w == "into" {
				return "SELECT INTO creates a table"
			}
			if w == "for" && i+1 < len(code) {
				switch strings.ToLower(code[i+1].Text) {
				case "update", "share", "no", "key":
					return "it locks rows (FOR UPDATE or FOR SHARE)"
				}
			}
			if i+1 < len(code) && code[i+1].Kind == Op && code[i+1].Text == "(" {
				if denied[w] || strings.HasPrefix(w, "dblink") || strings.HasPrefix(w, "lo_") {
					return fmt.Sprintf("it calls %s(), which acts outside the query", w)
				}
			}
		}
	}
	return ""
}
