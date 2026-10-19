// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	"context"
	"encoding/binary"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/proxy"
)

func TestLiterals(t *testing.T) {
	be := func(n int, v uint64) []byte {
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, v)
		return b[8-n:]
	}
	cases := []struct {
		v      []byte
		null   bool
		bin    bool
		oid    uint32
		want   string
		wantOK bool
	}{
		{[]byte("42"), false, false, 0, "'42'", true},
		{[]byte("O'Brien"), false, false, 25, "'O''Brien'::text", true},
		{[]byte(`a\b`), false, false, 0, `'a\b'`, true},
		{nil, true, false, 23, "NULL::int4", true},
		{nil, true, false, 0, "NULL", true},
		{be(4, uint64(uint32(0xFFFFFFFE))), false, true, 23, "'-2'::int4", true},
		{be(8, 1<<40), false, true, 20, "'1099511627776'::int8", true},
		{be(2, 7), false, true, 21, "'7'::int2", true},
		{[]byte{1}, false, true, 16, "true", true},
		{be(8, math.Float64bits(2.5)), false, true, 701, "'2.5'::float8", true},
		{[]byte("hi"), false, true, 25, "'hi'::text", true},
		{[]byte{0xde, 0xad}, false, true, 17, `'\xdead'::bytea`, true},
		{be(4, 1), false, true, 1082, "'2000-01-02'::date", true},
		{be(8, 1500000), false, true, 1184, "'2000-01-01 00:00:01.5+00'::timestamptz", true},
		{append([]byte{1}, `{"a":1}`...), false, true, 3802, `'{"a":1}'::jsonb`, true},
		{[]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, false, true, 2950, "'01020304-0506-0708-090a-0b0c0d0e0f10'::uuid", true},
		{[]byte{1, 2}, false, true, 0, "", false},
		{[]byte{1, 2}, false, true, 600, "", false},
	}
	for _, c := range cases {
		got, ok := literal(c.v, c.null, c.bin, c.oid)
		if got != c.want || ok != c.wantOK {
			t.Errorf("literal(%v, oid %d, binary %v) = %q %v, want %q %v", c.v, c.oid, c.bin, got, ok, c.want, c.wantOK)
		}
	}
}

func msg(parts ...any) []byte {
	var b []byte
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			b = append(b, v...)
			b = append(b, 0)
		case int16:
			b = binary.BigEndian.AppendUint16(b, uint16(v))
		case int32:
			b = binary.BigEndian.AppendUint32(b, uint32(v))
		case []byte:
			b = append(b, v...)
		}
	}
	return b
}

func TestSessionFollowsExtendedProtocol(t *testing.T) {
	c := NewCollector()
	defer c.Close()
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	// Parse with one int4 parameter, Bind in text, Execute, then a second
	// Bind and Execute pipelined before Sync; the second fails.
	s.FromClient('P', msg("s1", "SELECT * FROM t WHERE id = $1", int16(1), int32(23)))
	s.FromClient('B', msg("", "s1", int16(0), int16(1), int32(2), []byte("17"), int16(0)))
	s.FromClient('E', msg("", int32(0)))
	s.FromClient('B', msg("", "s1", int16(0), int16(1), int32(-1), int16(0)))
	s.FromClient('E', msg("", int32(0)))
	s.FromClient('E', msg("", int32(0)))
	s.FromClient('S', nil)
	s.FromServer('1', nil)
	s.FromServer('2', nil)
	s.FromServer('D', nil)
	s.FromServer('C', msg("SELECT 1"))
	s.FromServer('2', nil)
	s.FromServer('E', msg("SERROR", "C42P01", ""))
	s.FromServer('Z', []byte{'I'})
	// A simple query with two statements counts once.
	s.FromClient('Q', msg("SELECT 1; SELECT 2"))
	s.FromServer('C', msg("SELECT 1"))
	s.FromServer('C', msg("SELECT 1"))
	s.FromServer('Z', []byte{'I'})
	c.Flush()
	snap := c.Snapshot()
	var prepared, simple *Shape
	for i := range snap {
		if strings.Contains(snap[i].Normalized, "from t") {
			prepared = &snap[i]
		} else {
			simple = &snap[i]
		}
	}
	if prepared == nil || simple == nil {
		t.Fatalf("shapes: %+v", snap)
	}
	if prepared.Calls != 2 || prepared.Errors != 1 || prepared.Database != "shop" {
		t.Errorf("prepared: calls %d errors %d db %q", prepared.Calls, prepared.Errors, prepared.Database)
	}
	if len(prepared.Samples) != 1 || prepared.Samples[0].Text != "SELECT * FROM t WHERE id = '17'::int4" {
		t.Errorf("samples: %+v", prepared.Samples)
	}
	if simple.Calls != 1 || simple.Skip == "" {
		t.Errorf("simple: %+v", simple)
	}
}

func TestBinaryParamsTakeTypesFromDescribe(t *testing.T) {
	c := NewCollector()
	defer c.Close()
	s := c.Open(map[string]string{"user": "app"})
	s.FromClient('P', msg("", "SELECT $1 + 1", int16(0)))
	s.FromClient('D', msg("S"+""))
	s.FromClient('S', nil)
	s.FromServer('1', nil)
	s.FromServer('t', msg(int16(1), int32(20)))
	s.FromServer('T', nil)
	s.FromServer('Z', []byte{'I'})
	v := make([]byte, 8)
	binary.BigEndian.PutUint64(v, 5)
	s.FromClient('B', msg("", "", int16(1), int16(1), int16(1), int32(8), v, int16(0)))
	s.FromClient('E', msg("", int32(0)))
	s.FromClient('S', nil)
	s.FromServer('2', nil)
	s.FromServer('C', msg("SELECT 1"))
	s.FromServer('Z', []byte{'I'})
	c.Flush()
	snap := c.Snapshot()
	if len(snap) != 1 || len(snap[0].Samples) != 1 || snap[0].Samples[0].Text != "SELECT '5'::int8 + 1" {
		t.Errorf("%+v", snap)
	}
}

func TestPipelinedTimesDontIncludeTheWait(t *testing.T) {
	c := NewCollector()
	defer c.Close()
	s := c.Open(map[string]string{"user": "app"}).(*session)
	s.FromClient('P', msg("a", "SELECT pg_sleep(0)", int16(0)))
	for i := 0; i < 3; i++ {
		s.FromClient('B', msg("", "a", int16(0), int16(0), int16(0)))
		s.FromClient('E', msg("", int32(0)))
	}
	s.FromClient('S', nil)
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		s.FromServer('C', msg("SELECT 1"))
	}
	s.FromServer('Z', []byte{'I'})
	c.Flush()
	sh := c.Snapshot()[0]
	// Each took about 20 ms; charged from send time the last would be 60.
	if sh.Calls != 3 || sh.TotalMs > 90 || sh.TotalMs < 55 {
		t.Errorf("calls %d total %.1f ms", sh.Calls, sh.TotalMs)
	}
}

func TestOverflowDropsReportsNotTraffic(t *testing.T) {
	c := &Collector{ch: make(chan event, 1), cache: map[string]grouped{}, shapes: map[string]*Shape{}, done: make(chan struct{})}
	for i := 0; i < 5; i++ {
		c.record(event{text: "SELECT 1"})
	}
	if c.dropped.Load() != 4 {
		t.Errorf("dropped %d", c.dropped.Load())
	}
}

func TestSamplesKeepTheSlowest(t *testing.T) {
	s := &Shape{}
	for i := 0; i < 20; i++ {
		keepSample(s, Sample{Text: "q" + strconv.Itoa(i), Ms: float64(i)})
	}
	if len(s.Samples) != MaxSamples {
		t.Fatalf("%d samples", len(s.Samples))
	}
	for _, smp := range s.Samples {
		if smp.Ms < 12 {
			t.Errorf("kept a fast sample %v", smp)
		}
	}
}

func TestQuantiles(t *testing.T) {
	s := &Shape{}
	for i := 1; i <= 100; i++ {
		s.Calls++
		s.hist[bucket(float64(i))]++
	}
	if p := s.quantile(0.5); p < 40 || p > 60 {
		t.Errorf("p50 %.1f", p)
	}
	if p := s.quantile(0.95); p < 85 || p > 110 {
		t.Errorf("p95 %.1f", p)
	}
}

// Through a real proxy and server.

func startProxy(t *testing.T, c *Collector) (dsn string) {
	t.Helper()
	upstream := os.Getenv("LQO_TEST_DSN")
	if upstream == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	cfg, _ := pgwire.ParseConfig(upstream)
	cfg.SSLMode = "disable"
	ln, err := proxy.Listen("127.0.0.1:0", 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := proxy.New(proxy.Config{Upstream: cfg, Watch: c})
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx, ln)
	t.Cleanup(func() { cancel(); srv.Shutdown(time.Second) })
	u, _ := url.Parse(upstream)
	u.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))
	return u.String()
}

func TestThroughTheProxy(t *testing.T) {
	c := NewCollector()
	defer c.Close()
	dsn := startProxy(t, c)
	cfg, _ := pgwire.ParseConfig(dsn)
	conn, err := pgwire.Connect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		conn.Exec(ctx, "SELECT relname FROM pg_class WHERE oid = "+strconv.Itoa(1259+i))
		conn.ExecOne(ctx, "SELECT count(*) FROM pg_attribute WHERE attnum > "+strconv.Itoa(i))
	}
	conn.Close()
	if bin, err := exec.LookPath("pgbench"); err == nil {
		script := filepath.Join(t.TempDir(), "s.sql")
		os.WriteFile(script, []byte("\\set a random(1, 50)\nSELECT count(*) FROM pg_class WHERE relpages > :a;\n"), 0o600)
		out, err := exec.Command(bin, "-n", "-c", "2", "-t", "50", "-M", "prepared", "-f", script, dsn).CombinedOutput()
		if err != nil {
			t.Fatalf("pgbench: %v\n%s", err, out)
		}
	}
	c.Flush()
	got := map[string]*Shape{}
	snap := c.Snapshot()
	for i := range snap {
		got[snap[i].Normalized] = &snap[i]
	}
	if s := got["select relname from pg_class where oid = ?"]; s == nil || s.Calls != 5 || len(s.Samples) != 5 {
		t.Errorf("simple shape: %+v", s)
	}
	if s := got["select count ( * ) from pg_attribute where attnum > ?"]; s == nil || s.Calls != 5 {
		t.Errorf("extended shape: %+v", s)
	}
	if _, err := exec.LookPath("pgbench"); err == nil {
		s := got["select count ( * ) from pg_class where relpages > ?"]
		if s == nil || s.Calls != 100 || len(s.Samples) == 0 || !strings.Contains(s.Samples[0].Text, "relpages > '") {
			for k, v := range got {
				t.Logf("%q calls %d", k, v.Calls)
			}
			t.Errorf("pgbench shape: %+v", s)
		}
	}
	if st := c.Stats(); st.Dropped != 0 {
		t.Errorf("dropped %d", st.Dropped)
	}
}
