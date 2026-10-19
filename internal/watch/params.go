// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Type OIDs LQO knows how to write as SQL literals.
var typeNames = map[uint32]string{
	16: "bool", 17: "bytea", 19: "name", 20: "int8", 21: "int2", 23: "int4", 25: "text",
	26: "oid", 114: "json", 700: "float4", 701: "float8", 1042: "bpchar", 1043: "varchar",
	1082: "date", 1083: "time", 1114: "timestamp", 1184: "timestamptz", 1186: "interval",
	1700: "numeric", 2950: "uuid", 3802: "jsonb",
}

// literal turns one bound parameter into SQL text that means the same
// value: NULL, or a quoted literal with a cast when the type is known. ok is
// false for a binary value of a type LQO can't decode; the call then gives
// no sample text.
func literal(v []byte, null, binaryFmt bool, oid uint32) (string, bool) {
	if null {
		if name, ok := typeNames[oid]; ok {
			return "NULL::" + name, true
		}
		return "NULL", true
	}
	if !binaryFmt {
		return quoted(string(v), oid), true
	}
	switch oid {
	case 16:
		if len(v) != 1 {
			return "", false
		}
		if v[0] != 0 {
			return "true", true
		}
		return "false", true
	case 21:
		if len(v) != 2 {
			return "", false
		}
		return quoted(strconv.Itoa(int(int16(binary.BigEndian.Uint16(v)))), oid), true
	case 23:
		if len(v) != 4 {
			return "", false
		}
		return quoted(strconv.Itoa(int(int32(binary.BigEndian.Uint32(v)))), oid), true
	case 26:
		if len(v) != 4 {
			return "", false
		}
		return quoted(strconv.FormatUint(uint64(binary.BigEndian.Uint32(v)), 10), oid), true
	case 20:
		if len(v) != 8 {
			return "", false
		}
		return quoted(strconv.FormatInt(int64(binary.BigEndian.Uint64(v)), 10), oid), true
	case 700:
		if len(v) != 4 {
			return "", false
		}
		return quoted(fmtFloat(float64(math.Float32frombits(binary.BigEndian.Uint32(v))), 32), oid), true
	case 701:
		if len(v) != 8 {
			return "", false
		}
		return quoted(fmtFloat(math.Float64frombits(binary.BigEndian.Uint64(v)), 64), oid), true
	case 25, 1043, 1042, 19:
		return quoted(string(v), oid), true
	case 17:
		return quoted(`\x`+hex.EncodeToString(v), oid), true
	case 2950:
		if len(v) != 16 {
			return "", false
		}
		h := hex.EncodeToString(v)
		return quoted(h[0:8]+"-"+h[8:12]+"-"+h[12:16]+"-"+h[16:20]+"-"+h[20:], oid), true
	case 1082:
		if len(v) != 4 {
			return "", false
		}
		days := int32(binary.BigEndian.Uint32(v))
		if days == math.MaxInt32 || days == math.MinInt32 {
			return "", false // infinity
		}
		return quoted(pgEpoch.AddDate(0, 0, int(days)).Format("2006-01-02"), oid), true
	case 1114, 1184:
		if len(v) != 8 {
			return "", false
		}
		us := int64(binary.BigEndian.Uint64(v))
		if us == math.MaxInt64 || us == math.MinInt64 {
			return "", false
		}
		t := pgEpoch.Add(time.Duration(us) * time.Microsecond)
		s := t.Format("2006-01-02 15:04:05.999999")
		if oid == 1184 {
			s += "+00"
		}
		return quoted(s, oid), true
	case 3802:
		if len(v) < 1 || v[0] != 1 {
			return "", false
		}
		return quoted(string(v[1:]), oid), true
	case 114:
		return quoted(string(v), oid), true
	}
	return "", false
}

var pgEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func fmtFloat(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	return strconv.FormatFloat(f, 'g', -1, bits)
}

// quoted is a standard string literal; the proxy's trials run only with
// standard_conforming_strings on, where a backslash is an ordinary
// character and a quote is doubled.
func quoted(s string, oid uint32) string {
	lit := "'" + strings.ReplaceAll(s, "'", "''") + "'"
	if name, ok := typeNames[oid]; ok {
		return lit + "::" + name
	}
	return lit
}

// bindMessage is a parsed Bind.
type bindMessage struct {
	portal, stmt string
	formats      []int16
	values       [][]byte // nil entry: NULL
}

func parseBind(b []byte) (*bindMessage, error) {
	m := &bindMessage{}
	var ok bool
	if m.portal, b, ok = cstring(b); !ok {
		return nil, errShort
	}
	if m.stmt, b, ok = cstring(b); !ok {
		return nil, errShort
	}
	if len(b) < 2 {
		return nil, errShort
	}
	nf := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) < 2*nf {
		return nil, errShort
	}
	for i := 0; i < nf; i++ {
		m.formats = append(m.formats, int16(binary.BigEndian.Uint16(b[2*i:])))
	}
	b = b[2*nf:]
	if len(b) < 2 {
		return nil, errShort
	}
	np := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	for i := 0; i < np; i++ {
		if len(b) < 4 {
			return nil, errShort
		}
		n := int32(binary.BigEndian.Uint32(b))
		b = b[4:]
		if n < 0 {
			m.values = append(m.values, nil)
			continue
		}
		if int(n) > len(b) {
			return nil, errShort
		}
		m.values = append(m.values, append([]byte{}, b[:n]...))
		b = b[n:]
	}
	return m, nil
}

func (m *bindMessage) binary(i int) bool {
	switch len(m.formats) {
	case 0:
		return false
	case 1:
		return m.formats[0] == 1
	}
	return i < len(m.formats) && m.formats[i] == 1
}

var errShort = fmt.Errorf("short message")

func cstring(b []byte) (string, []byte, bool) {
	for i, c := range b {
		if c == 0 {
			return string(b[:i]), b[i+1:], true
		}
	}
	return "", nil, false
}
