// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package proxy is LQO's PostgreSQL proxy. It sits between an application and
// its database server and relays the wire protocol in both directions, one
// whole message at a time, without changing what the two sides say to each
// other. The application logs in to the real server through it: the proxy
// holds no passwords.
//
// When the application connects with TLS, the proxy ends that TLS with its
// own certificate and opens its own connection to the server, with TLS if the
// upstream connection string asks for it. It has to: the point of the proxy
// is to see the queries.
package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
)

// Request codes a client can send in place of a startup message.
const (
	codeCancel = 80877102
	codeSSL    = 80877103
	codeGSSENC = 80877104
)

// maxStartup is the largest startup packet accepted, as in PostgreSQL.
const maxStartup = 10000

// maxMessage is the largest message relayed. PostgreSQL's own limit for most
// messages is 1 GB; rows and COPY data can come close to it.
const maxMessage = 1<<30 + 1<<20

// DefaultPort is the port LQO listens on unless told otherwise: next to
// PgBouncer's 6432, without taking it.
const DefaultPort = 6433

// Config sets up a proxy.
type Config struct {
	// Upstream is where the real server is. Its host, port and TLS settings
	// are used; the user, database and password come from each client.
	Upstream *pgwire.Config
	// TLS, when set, lets clients connect with TLS, ended at the proxy.
	TLS *tls.Config
	// Watch, when set, is told about every message relayed. It must not
	// block; see Watcher.
	Watch Watcher
	// StartupTimeout limits how long a client may take to log in.
	StartupTimeout time.Duration
	// Logf, when set, receives one line per notable event. Query text is
	// never logged.
	Logf func(format string, args ...any)
}

// Watcher is told about the traffic the proxy relays. The proxy calls it on
// the relaying goroutine, so it must return quickly and must not keep body,
// which is reused after the call.
type Watcher interface {
	// Open is called once a client has logged in; the returned value is
	// passed back with every message of that session.
	Open(params map[string]string) Session
}

// Session watches one client connection.
type Session interface {
	// FromClient is called for each message the client sends.
	FromClient(typ byte, body []byte)
	// FromServer is called for each message the server sends.
	FromServer(typ byte, body []byte)
	// Close is called when the connection ends.
	Close()
}

// Server is a running proxy.
type Server struct {
	cfg      Config
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
	closing  atomic.Bool
	sessions atomic.Int64
	total    atomic.Int64
}

// New returns a proxy for cfg.
func New(cfg Config) *Server {
	if cfg.StartupTimeout == 0 {
		cfg.StartupTimeout = 60 * time.Second
	}
	return &Server{cfg: cfg, conns: map[net.Conn]struct{}{}}
}

// Listen opens the address to serve on. A path that starts with / is a
// directory: the proxy then listens on the Unix socket .s.PGSQL.<port> in it,
// where libpq looks for one, so clients connect with host=<dir> port=<port>.
func Listen(addr string, port int) (net.Listener, error) {
	if strings.HasPrefix(addr, "/") {
		path := filepath.Join(addr, ".s.PGSQL."+strconv.Itoa(port))
		if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSocket != 0 {
			// A socket left by a proxy that didn't shut down cleanly. If
			// something answers on it, it's in use.
			if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
				c.Close()
				return nil, fmt.Errorf("%s is in use", path)
			}
			os.Remove(path)
		}
		return net.Listen("unix", path)
	}
	if !strings.Contains(addr, ":") {
		addr = net.JoinHostPort(addr, strconv.Itoa(port))
	}
	return net.Listen("tcp", addr)
}

// Active is the number of client connections open now.
func (s *Server) Active() int64 { return s.sessions.Load() }

// Total is the number of client connections accepted so far.
func (s *Server) Total() int64 { return s.total.Load() }

// Serve accepts clients on ln until ctx ends or ln is closed. Connections
// already open carry on; Shutdown ends them.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	stop := context.AfterFunc(ctx, func() {
		s.closing.Store(true)
		ln.Close()
	})
	defer stop()
	var delay time.Duration
	for {
		c, err := ln.Accept()
		if err != nil {
			if s.closing.Load() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			// Out of file descriptors and the like: wait and try again,
			// as net/http does.
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else if delay *= 2; delay > time.Second {
				delay = time.Second
			}
			s.logf("accept: %v", err)
			time.Sleep(delay)
			continue
		}
		delay = 0
		s.track(c, true)
		s.wg.Add(1)
		s.total.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.track(c, false)
			s.handle(ctx, c)
		}()
	}
}

// Shutdown waits up to grace for open connections to end, then closes the
// rest. It returns the number it had to close.
func (s *Server) Shutdown(grace time.Duration) int {
	s.closing.Store(true)
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return 0
	case <-time.After(grace):
	}
	s.mu.Lock()
	n := len(s.conns)
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	<-done
	return n
}

func (s *Server) track(c net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.conns[c] = struct{}{}
	} else {
		delete(s.conns, c)
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.cfg.Logf != nil {
		s.cfg.Logf(format, args...)
	}
}

// handle serves one client connection from its first byte to its last.
func (s *Server) handle(ctx context.Context, raw net.Conn) {
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(s.cfg.StartupTimeout))

	var client net.Conn = raw
	br := bufio.NewReaderSize(raw, 16*1024)
	clientTLS := false
	var startup []byte
	for startup == nil {
		first, err := br.Peek(1)
		if err != nil {
			return
		}
		if first[0] == 0x16 {
			// A TLS handshake straight away, without SSLRequest: what
			// libpq 17 does with sslnegotiation=direct. It must ask for
			// the "postgresql" protocol by ALPN.
			if s.cfg.TLS == nil || clientTLS {
				return
			}
			conf := s.cfg.TLS.Clone()
			conf.NextProtos = []string{"postgresql"}
			tc := tls.Server(&bufferedConn{Conn: raw, r: br}, conf)
			if err := tc.HandshakeContext(ctx); err != nil {
				s.logf("TLS from %s: %v", raw.RemoteAddr(), err)
				return
			}
			if tc.ConnectionState().NegotiatedProtocol != "postgresql" {
				return
			}
			client, br, clientTLS = tc, bufio.NewReaderSize(tc, 16*1024), true
			continue
		}
		pkt, err := readStartup(br)
		if err != nil {
			s.logf("startup from %s: %v", raw.RemoteAddr(), err)
			return
		}
		switch code := binary.BigEndian.Uint32(pkt[4:8]); code {
		case codeSSL:
			if clientTLS {
				return
			}
			if br.Buffered() > 0 {
				// Bytes sent after SSLRequest before the answer could be
				// injected into the TLS session (CVE-2021-23214).
				return
			}
			if s.cfg.TLS == nil {
				raw.Write([]byte{'N'})
				continue
			}
			raw.Write([]byte{'S'})
			tc := tls.Server(raw, s.cfg.TLS)
			if err := tc.HandshakeContext(ctx); err != nil {
				s.logf("TLS from %s: %v", raw.RemoteAddr(), err)
				return
			}
			client, br, clientTLS = tc, bufio.NewReaderSize(tc, 16*1024), true
		case codeGSSENC:
			if br.Buffered() > 0 {
				return
			}
			client.Write([]byte{'N'})
		case codeCancel:
			s.forwardCancel(ctx, pkt)
			return
		default:
			if code>>16 != 3 {
				sendFatal(client, "08P01", fmt.Sprintf("unsupported frontend protocol %d.%d", code>>16, code&0xffff))
				return
			}
			startup = pkt
		}
	}
	params := parseStartup(startup)

	upCtx, cancel := context.WithTimeout(ctx, s.cfg.StartupTimeout)
	up, upTLS, err := pgwire.DialServer(upCtx, s.cfg.Upstream)
	cancel()
	if err != nil {
		s.logf("upstream: %v", err)
		sendFatal(client, "08006", "LQO couldn't reach the database server: "+err.Error())
		return
	}
	defer up.Close()
	if _, err := up.Write(startup); err != nil {
		sendFatal(client, "08006", "LQO lost the database server: "+err.Error())
		return
	}

	s.sessions.Add(1)
	defer s.sessions.Add(-1)

	var watch Session
	sasl := &saslState{upTLS: upTLS}
	var loggedIn atomic.Bool
	ready := make(chan struct{})
	var readyOnce sync.Once
	stopped := make(chan struct{})

	upR := bufio.NewReaderSize(up, 32*1024)
	upW := bufio.NewWriterSize(up, 32*1024)
	clW := bufio.NewWriterSize(client, 32*1024)

	errc := make(chan error, 2)
	// Server to client.
	go func() {
		errc <- relay(clW, upR, func(typ byte, body []byte) ([]byte, error) {
			if !loggedIn.Load() {
				switch typ {
				case 'R':
					return sasl.fromServer(body)
				case 'Z':
					// Logged in: no more deadline, and the watcher
					// starts.
					raw.SetDeadline(time.Time{})
					if s.cfg.Watch != nil {
						watch = s.cfg.Watch.Open(params)
					}
					loggedIn.Store(true)
					readyOnce.Do(func() { close(ready) })
				}
				return body, nil
			}
			if watch != nil {
				watch.FromServer(typ, body)
			}
			return body, nil
		})
	}()
	// Client to server.
	go func() {
		errc <- relay(upW, br, func(typ byte, body []byte) ([]byte, error) {
			if !loggedIn.Load() {
				if typ == 'p' {
					if err := sasl.fromClient(body); err != nil {
						sendFatal(client, "28000", err.Error())
						return nil, err
					}
				}
				if typ != 'p' && typ != 'X' {
					// Nothing but login answers may come before the
					// server says it's ready; wait for that.
					select {
					case <-ready:
					case <-stopped:
						return nil, io.EOF
					}
				}
			}
			if loggedIn.Load() && watch != nil {
				watch.FromClient(typ, body)
			}
			return body, nil
		})
	}()
	<-errc
	// Either side ending ends both.
	close(stopped)
	client.Close()
	up.Close()
	<-errc
	if watch != nil {
		watch.Close()
	}
}

// relay copies whole messages from src to dst until either side fails.
// hook sees each one and may replace its body. dst is flushed whenever src
// has nothing more buffered, so pipelined messages go out together.
func relay(dst *bufio.Writer, src *bufio.Reader, hook func(byte, []byte) ([]byte, error)) error {
	var hdr [5]byte
	buf := make([]byte, 8192)
	for {
		if _, err := io.ReadFull(src, hdr[:]); err != nil {
			dst.Flush()
			return err
		}
		n := int(binary.BigEndian.Uint32(hdr[1:])) - 4
		if n < 0 || n > maxMessage {
			return fmt.Errorf("bad message length %d", n)
		}
		body := buf[:0]
		if n > cap(buf) {
			if n <= 1<<20 {
				buf = make([]byte, n)
				body = buf
			} else {
				body = make([]byte, n) // don't keep huge buffers around
			}
		}
		body = body[:n]
		if _, err := io.ReadFull(src, body); err != nil {
			return err
		}
		out, err := hook(hdr[0], body)
		if err != nil {
			dst.Flush()
			return err
		}
		if len(out) != n {
			binary.BigEndian.PutUint32(hdr[1:], uint32(len(out)+4))
		}
		if _, err := dst.Write(hdr[:]); err != nil {
			return err
		}
		if _, err := dst.Write(out); err != nil {
			return err
		}
		if src.Buffered() == 0 {
			if err := dst.Flush(); err != nil {
				return err
			}
		}
	}
}

// saslState follows a SCRAM login as it passes through. Channel binding
// ties a login to one TLS connection, and the proxy makes two, so it can't
// work through the proxy: the proxy takes SCRAM-SHA-256-PLUS off the
// server's list, and refuses a client that would only fail later.
type saslState struct {
	upTLS bool
	// expectInitial is set when the server has offered SASL, so the
	// client's next password message is its SASLInitialResponse.
	expectInitial atomic.Bool
}

func (st *saslState) fromServer(body []byte) ([]byte, error) {
	if len(body) < 4 || binary.BigEndian.Uint32(body) != 10 {
		return body, nil
	}
	var mechs []string
	for _, m := range strings.Split(string(body[4:]), "\x00") {
		if m != "" && !strings.HasSuffix(m, "-PLUS") {
			mechs = append(mechs, m)
		}
	}
	out := binary.BigEndian.AppendUint32(nil, 10)
	for _, m := range mechs {
		out = append(out, m...)
		out = append(out, 0)
	}
	out = append(out, 0)
	st.expectInitial.Store(true)
	return out, nil
}

func (st *saslState) fromClient(body []byte) error {
	if !st.expectInitial.Swap(false) {
		return nil
	}
	i := strings.IndexByte(string(body), 0)
	if i < 0 || len(body) < i+5 {
		return nil
	}
	mech := string(body[:i])
	data := body[i+5:]
	if strings.HasSuffix(mech, "-PLUS") {
		return errors.New("SCRAM channel binding can't work through LQO; connect with channel_binding=disable or prefer")
	}
	if len(data) > 0 && data[0] == 'y' && st.upTLS {
		// The client supports channel binding but didn't see it offered.
		// The server, on its own TLS connection, does offer it and will
		// take this for a downgrade attack.
		return errors.New("SCRAM through LQO needs channel_binding=disable when the client connects to LQO with TLS; or connect to LQO without TLS, over a Unix socket or localhost")
	}
	return nil
}

// forwardCancel passes a cancel request on to the server. The key in it is
// the one the server gave the client, which the proxy relayed unchanged.
func (s *Server) forwardCancel(ctx context.Context, pkt []byte) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	up, _, err := pgwire.DialServer(ctx, s.cfg.Upstream)
	if err != nil {
		s.logf("cancel: %v", err)
		return
	}
	defer up.Close()
	up.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := up.Write(pkt); err != nil {
		return
	}
	io.Copy(io.Discard, up)
}

func readStartup(r *bufio.Reader) ([]byte, error) {
	var lenb [4]byte
	if _, err := io.ReadFull(r, lenb[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint32(lenb[:]))
	if n < 8 || n > maxStartup {
		return nil, fmt.Errorf("bad startup packet length %d", n)
	}
	pkt := make([]byte, n)
	copy(pkt, lenb[:])
	if _, err := io.ReadFull(r, pkt[4:]); err != nil {
		return nil, err
	}
	return pkt, nil
}

func parseStartup(pkt []byte) map[string]string {
	params := map[string]string{}
	parts := strings.Split(string(pkt[8:]), "\x00")
	for i := 0; i+1 < len(parts); i += 2 {
		if parts[i] == "" {
			break
		}
		params[parts[i]] = parts[i+1]
	}
	return params
}

// sendFatal sends an ErrorResponse, which a client shows as the reason its
// connection failed.
func sendFatal(w io.Writer, code, msg string) {
	b := []byte{'E', 0, 0, 0, 0}
	for _, f := range []struct {
		k byte
		v string
	}{{'S', "FATAL"}, {'V', "FATAL"}, {'C', code}, {'M', msg}} {
		b = append(b, f.k)
		b = append(b, f.v...)
		b = append(b, 0)
	}
	b = append(b, 0)
	binary.BigEndian.PutUint32(b[1:], uint32(len(b)-1))
	w.Write(b)
}

// bufferedConn is a net.Conn whose first reads come from a bufio.Reader
// that already holds some of its bytes.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
