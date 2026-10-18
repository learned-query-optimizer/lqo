// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package sqltext reads PostgreSQL SQL as text: it splits scripts into
// statements, normalises a statement into a shape that ignores its literal
// values, and decides whether a statement is safe for LQO to run.
package sqltext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind is a token's kind.
type Kind int

const (
	Word    Kind = iota // keyword or plain identifier
	Ident               // "quoted identifier"
	String              // 'text', E'text', U&'text', $$text$$, B'101', X'ff'
	Number              // 42, 3.14, 1e10, 0x1f
	Param               // $1
	Op                  // operators and punctuation
	Comment             // -- and /* */
	Space
)

// Token is one piece of SQL.
type Token struct {
	Kind Kind
	Text string
}

// Lex splits sql into tokens. It never fails: text it can't make sense of
// comes back as operators, and an unterminated string or comment runs to the
// end of the input.
func Lex(sql string) []Token {
	var out []Token
	i := 0
	n := len(sql)
	for i < n {
		c := sql[i]
		start := i
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			for i < n && strings.IndexByte(" \t\n\r\f", sql[i]) >= 0 {
				i++
			}
			out = append(out, Token{Space, sql[start:i]})
		case c == '-' && i+1 < n && sql[i+1] == '-':
			// PostgreSQL ends a line comment at a newline or a carriage return.
			for i < n && sql[i] != '\n' && sql[i] != '\r' {
				i++
			}
			out = append(out, Token{Comment, sql[start:i]})
		case c == '/' && i+1 < n && sql[i+1] == '*':
			depth := 0
			for i < n {
				if i+1 < n && sql[i] == '/' && sql[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if i+1 < n && sql[i] == '*' && sql[i+1] == '/' {
					depth--
					i += 2
					if depth == 0 {
						break
					}
					continue
				}
				i++
			}
			out = append(out, Token{Comment, sql[start:i]})
		case c == '\'':
			i = scanQuoted(sql, i+1, '\'', false)
			out = append(out, Token{String, sql[start:i]})
		case (c == 'E' || c == 'e') && i+1 < n && sql[i+1] == '\'':
			i = scanQuoted(sql, i+2, '\'', true)
			out = append(out, Token{String, sql[start:i]})
		case (c == 'B' || c == 'b' || c == 'X' || c == 'x' || c == 'N' || c == 'n') && i+1 < n && sql[i+1] == '\'':
			i = scanQuoted(sql, i+2, '\'', false)
			out = append(out, Token{String, sql[start:i]})
		case (c == 'U' || c == 'u') && i+2 < n && sql[i+1] == '&' && (sql[i+2] == '\'' || sql[i+2] == '"'):
			q := sql[i+2]
			i = scanQuoted(sql, i+3, q, false)
			k := String
			if q == '"' {
				k = Ident
			}
			out = append(out, Token{k, sql[start:i]})
		case c == '"':
			i = scanQuoted(sql, i+1, '"', false)
			out = append(out, Token{Ident, sql[start:i]})
		case c == '$':
			if i+1 < n && sql[i+1] >= '0' && sql[i+1] <= '9' {
				i++
				for i < n && sql[i] >= '0' && sql[i] <= '9' {
					i++
				}
				out = append(out, Token{Param, sql[start:i]})
				break
			}
			if tag, ok := dollarTag(sql, i); ok {
				end := strings.Index(sql[i+len(tag):], tag)
				if end < 0 {
					i = n
				} else {
					i = i + len(tag) + end + len(tag)
				}
				out = append(out, Token{String, sql[start:i]})
				break
			}
			i++
			out = append(out, Token{Op, sql[start:i]})
		case c >= '0' && c <= '9' || (c == '.' && i+1 < n && sql[i+1] >= '0' && sql[i+1] <= '9'):
			i = scanNumber(sql, i)
			out = append(out, Token{Number, sql[start:i]})
		case isIdentStart(sql, i):
			for i < n {
				r, size := utf8.DecodeRuneInString(sql[i:])
				if r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) || r >= 0x80 {
					i += size
					continue
				}
				break
			}
			out = append(out, Token{Word, sql[start:i]})
		default:
			i = scanOp(sql, i)
			out = append(out, Token{Op, sql[start:i]})
		}
	}
	return out
}

func isIdentStart(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r == '_' || unicode.IsLetter(r) || r >= 0x80
}

// scanQuoted returns the index after the closing quote. A doubled quote is
// an escaped quote; with backslash set, \x escapes x as well.
func scanQuoted(s string, i int, q byte, backslash bool) int {
	n := len(s)
	for i < n {
		c := s[i]
		if backslash && c == '\\' && i+1 < n {
			i += 2
			continue
		}
		if c == q {
			if i+1 < n && s[i+1] == q {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return n
}

// dollarTag reads a $tag$ opener at i.
func dollarTag(s string, i int) (string, bool) {
	j := i + 1
	for j < len(s) {
		c := s[j]
		if c == '$' {
			return s[i : j+1], true
		}
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' && j > i+1 || c >= 0x80) {
			return "", false
		}
		j++
	}
	return "", false
}

func scanNumber(s string, i int) int {
	n := len(s)
	if s[i] == '0' && i+1 < n && strings.IndexByte("xXoObB", s[i+1]) >= 0 {
		i += 2
		for i < n && (isHex(s[i]) || s[i] == '_') {
			i++
		}
		return i
	}
	for i < n && (s[i] >= '0' && s[i] <= '9' || s[i] == '_') {
		i++
	}
	if i < n && s[i] == '.' && !(i+1 < n && s[i+1] == '.') {
		i++
		for i < n && (s[i] >= '0' && s[i] <= '9' || s[i] == '_') {
			i++
		}
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < n && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < n && s[j] >= '0' && s[j] <= '9' {
			i = j
			for i < n && s[i] >= '0' && s[i] <= '9' {
				i++
			}
		}
	}
	return i
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

const opChars = "+-*/<>=~!@#%^&|`?"

func scanOp(s string, i int) int {
	if strings.IndexByte(opChars, s[i]) < 0 {
		_, size := utf8.DecodeRuneInString(s[i:])
		if s[i] == ':' && i+1 < len(s) && s[i+1] == ':' {
			return i + 2
		}
		return i + size
	}
	j := i
	for j < len(s) && strings.IndexByte(opChars, s[j]) >= 0 {
		if j+1 < len(s) && (s[j] == '-' && s[j+1] == '-' || s[j] == '/' && s[j+1] == '*') {
			break
		}
		j++
	}
	if j == i {
		j = i + 1
	}
	return j
}

// Piece is one statement cut from a script, with the line it starts on.
type Piece struct {
	Text string
	Line int
}

// Split cuts a script into statements at semicolons outside strings,
// identifiers and comments. Empty statements are dropped. Each statement
// keeps its own text, without the semicolon, trimmed.
func Split(script string) []string {
	var out []string
	for _, p := range SplitLines(script) {
		out = append(out, p.Text)
	}
	return out
}

// SplitLines is Split, with the line each statement's code starts on.
func SplitLines(script string) []Piece {
	var out []Piece
	var cur strings.Builder
	depth := 0 // parentheses, so a semicolon inside them doesn't split
	line := 1
	start := 0
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" && hasCode(s) {
			out = append(out, Piece{s, start})
		}
		cur.Reset()
		start = 0
	}
	for _, t := range Lex(script) {
		if t.Kind == Op {
			switch t.Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			case ";":
				if depth == 0 {
					flush()
					continue
				}
			}
		}
		if start == 0 && t.Kind != Space && t.Kind != Comment {
			start = line
		}
		cur.WriteString(t.Text)
		line += strings.Count(t.Text, "\n")
	}
	flush()
	return out
}

// hasCode reports whether s holds anything but comments and spaces.
func hasCode(s string) bool {
	for _, t := range Lex(s) {
		if t.Kind != Comment && t.Kind != Space {
			return true
		}
	}
	return false
}

// Code returns the tokens of sql without comments and spaces.
func Code(sql string) []Token {
	var out []Token
	for _, t := range Lex(sql) {
		if t.Kind != Comment && t.Kind != Space {
			out = append(out, t)
		}
	}
	return out
}

// TrimLeadingComments drops comments and spaces before a statement's first
// token, such as the notes a SQL file puts above each query.
func TrimLeadingComments(sql string) string {
	toks := Lex(sql)
	i := 0
	n := 0
	for i < len(toks) && (toks[i].Kind == Comment || toks[i].Kind == Space) {
		n += len(toks[i].Text)
		i++
	}
	return strings.TrimSpace(sql[n:])
}
