// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package pgwire

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PgError is an error the server sent back.
type PgError struct {
	Severity string
	Code     string // SQLSTATE, such as 57014 for a cancelled statement
	Message  string
	Detail   string
	Hint     string
	Position string
}

func (e *PgError) Error() string {
	s := fmt.Sprintf("%s: %s (SQLSTATE %s)", e.Severity, e.Message, e.Code)
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	return s
}

// SQLSTATE codes LQO looks for.
const (
	CodeQueryCanceled  = "57014" // statement_timeout fired, or a cancel request
	CodeReadOnlySQL    = "25006" // a write inside a READ ONLY transaction
	CodeLockNotAvail   = "55P03" // lock_timeout fired
	CodeUndefinedParam = "42P02" // $1 with nothing bound to it
)

// IsCode reports whether err is a server error with the given SQLSTATE.
func IsCode(err error, code string) bool {
	var pe *PgError
	return errors.As(err, &pe) && pe.Code == code
}

// Result is what one statement in a query returned.
type Result struct {
	Columns []string
	Rows    [][]*string // nil marks NULL
	Tag     string      // the command tag, such as "SELECT 3"
}

// Conn is one connection. It isn't safe for concurrent use.
type Conn struct {
	cfg      *Config
	netConn  net.Conn
	r        *bufio.Reader
	pid      uint32
	secret   []byte
	params   map[string]string
	txStatus byte
	broken   bool
	usedTLS  bool
	tlsConf  *tls.Config
	Notices  []string
}

// Connect opens a connection and logs in.
func Connect(ctx context.Context, cfg *Config) (*Conn, error) {
	if cfg.ConnectTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.ConnectTimeout)
		defer cancel()
	}
	c := &Conn{cfg: cfg, params: map[string]string{}}
	nc, err := c.dial(ctx, cfg.SSLMode != "disable" && cfg.SSLMode != "allow")
	if err != nil {
		return nil, err
	}
	c.netConn = nc
	if dl, ok := ctx.Deadline(); ok {
		nc.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { nc.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	c.r = bufio.NewReaderSize(nc, 32*1024)
	if err := c.startup(); err != nil {
		nc.Close()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("connecting to %s: %w", cfg.Describe(), ctx.Err())
		}
		return nil, err
	}
	nc.SetDeadline(time.Time{})
	return c, nil
}

// dial opens the socket and, when wanted and offered, switches it to TLS.
func (c *Conn) dial(ctx context.Context, wantTLS bool) (net.Conn, error) {
	network, addr := c.cfg.Address()
	var d net.Dialer
	nc, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", c.cfg.Describe(), err)
	}
	if !wantTLS || c.cfg.IsUnixSocket() {
		if c.cfg.SSLMode == "require" || strings.HasPrefix(c.cfg.SSLMode, "verify") {
			if c.cfg.IsUnixSocket() {
				nc.Close()
				return nil, fmt.Errorf("sslmode=%s can't be used over a Unix socket", c.cfg.SSLMode)
			}
		}
		return nc, nil
	}
	if dl, ok := ctx.Deadline(); ok {
		nc.SetDeadline(dl)
	}
	req := make([]byte, 8)
	binary.BigEndian.PutUint32(req[0:], 8)
	binary.BigEndian.PutUint32(req[4:], 80877103)
	if _, err := nc.Write(req); err != nil {
		nc.Close()
		return nil, fmt.Errorf("connecting to %s: %w", c.cfg.Describe(), err)
	}
	ans := make([]byte, 1)
	if _, err := io.ReadFull(nc, ans); err != nil {
		nc.Close()
		return nil, fmt.Errorf("connecting to %s: %w", c.cfg.Describe(), err)
	}
	if ans[0] != 'S' {
		if c.cfg.SSLMode == "prefer" {
			return nc, nil
		}
		nc.Close()
		return nil, fmt.Errorf("connecting to %s: the server doesn't offer TLS and sslmode is %s", c.cfg.Describe(), c.cfg.SSLMode)
	}
	conf, err := c.tlsConfig()
	if err != nil {
		nc.Close()
		return nil, err
	}
	tc := tls.Client(nc, conf)
	if err := tc.HandshakeContext(ctx); err != nil {
		nc.Close()
		return nil, fmt.Errorf("TLS with %s: %w", c.cfg.Describe(), err)
	}
	c.usedTLS = true
	c.tlsConf = conf
	return tc, nil
}

func (c *Conn) tlsConfig() (*tls.Config, error) {
	cfg := c.cfg
	conf := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	rootPath := cfg.SSLRootCert
	if rootPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			p := filepath.Join(home, ".postgresql", "root.crt")
			if _, err := os.Stat(p); err == nil {
				rootPath = p
			}
		}
	}
	mode := cfg.SSLMode
	if mode == "require" && rootPath != "" && cfg.SSLRootCert != "" {
		mode = "verify-ca" // libpq does the same when a root certificate is given
	}
	var roots *x509.CertPool
	if mode == "verify-ca" || mode == "verify-full" {
		if rootPath == "" || rootPath == "system" {
			sys, err := x509.SystemCertPool()
			if err != nil {
				return nil, fmt.Errorf("reading system certificates: %w", err)
			}
			roots = sys
		} else {
			pem, err := os.ReadFile(rootPath)
			if err != nil {
				return nil, fmt.Errorf("reading sslrootcert: %w", err)
			}
			roots = x509.NewCertPool()
			if !roots.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("no certificates found in %s", rootPath)
			}
		}
	}
	switch mode {
	case "prefer", "require", "allow":
		conf.InsecureSkipVerify = true
	case "verify-full":
		conf.RootCAs = roots
	case "verify-ca":
		// Check the chain but not the host name, as libpq does.
		conf.InsecureSkipVerify = true
		conf.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("the server sent no certificate")
			}
			certs := make([]*x509.Certificate, len(raw))
			for i, b := range raw {
				cert, err := x509.ParseCertificate(b)
				if err != nil {
					return err
				}
				certs[i] = cert
			}
			inter := x509.NewCertPool()
			for _, cert := range certs[1:] {
				inter.AddCert(cert)
			}
			_, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter})
			return err
		}
	}
	if cfg.SSLCert != "" {
		key := cfg.SSLKey
		if key == "" {
			key = strings.TrimSuffix(cfg.SSLCert, ".crt") + ".key"
		}
		pair, err := tls.LoadX509KeyPair(cfg.SSLCert, key)
		if err != nil {
			return nil, fmt.Errorf("reading the client certificate: %w", err)
		}
		conf.Certificates = []tls.Certificate{pair}
	}
	return conf, nil
}

func (c *Conn) startup() error {
	var b []byte
	b = binary.BigEndian.AppendUint32(b, 0)
	b = binary.BigEndian.AppendUint32(b, 196608) // protocol 3.0
	add := func(k, v string) {
		if v != "" {
			b = append(b, k...)
			b = append(b, 0)
			b = append(b, v...)
			b = append(b, 0)
		}
	}
	add("user", c.cfg.User)
	add("database", c.cfg.Database)
	app := c.cfg.ApplicationName
	if app == "" {
		app = "lqo"
	}
	add("application_name", app)
	add("client_encoding", "UTF8")
	add("options", c.cfg.Options)
	b = append(b, 0)
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	if _, err := c.netConn.Write(b); err != nil {
		return err
	}
	for {
		typ, msg, err := c.readMsg()
		if err != nil {
			return err
		}
		switch typ {
		case 'R':
			if err := c.authenticate(msg); err != nil {
				return err
			}
		case 'S':
			c.paramStatus(msg)
		case 'K':
			if len(msg) >= 8 {
				c.pid = binary.BigEndian.Uint32(msg)
				c.secret = append([]byte(nil), msg[4:]...)
			}
		case 'Z':
			if len(msg) > 0 {
				c.txStatus = msg[0]
			}
			return nil
		case 'E':
			return parseError(msg)
		case 'N':
			c.Notices = append(c.Notices, parseError(msg).Message)
		case 'v': // NegotiateProtocolVersion: carry on with 3.0
		default:
			return fmt.Errorf("unexpected message %q while logging in", typ)
		}
	}
}

func (c *Conn) authenticate(msg []byte) error {
	if len(msg) < 4 {
		return errors.New("short authentication message")
	}
	code := binary.BigEndian.Uint32(msg)
	switch code {
	case 0:
		return nil
	case 3:
		if c.cfg.Password == "" {
			return errors.New("the server wants a password and none was given")
		}
		return c.sendPassword(c.cfg.Password)
	case 5:
		if c.cfg.Password == "" {
			return errors.New("the server wants a password and none was given")
		}
		if len(msg) < 8 {
			return errors.New("short md5 authentication message")
		}
		h1 := md5.Sum([]byte(c.cfg.Password + c.cfg.User))
		inner := hex.EncodeToString(h1[:])
		h2 := md5.Sum(append([]byte(inner), msg[4:8]...))
		return c.sendPassword("md5" + hex.EncodeToString(h2[:]))
	case 10:
		if c.cfg.Password == "" {
			return errors.New("the server wants a password and none was given")
		}
		return c.scram(msg[4:])
	default:
		return fmt.Errorf("the server asked for an authentication method LQO doesn't support (code %d)", code)
	}
}

func (c *Conn) sendPassword(p string) error {
	b := []byte{'p', 0, 0, 0, 0}
	b = append(b, p...)
	b = append(b, 0)
	binary.BigEndian.PutUint32(b[1:], uint32(len(b)-1))
	_, err := c.netConn.Write(b)
	return err
}

func (c *Conn) paramStatus(msg []byte) {
	parts := strings.SplitN(string(msg), "\x00", 3)
	if len(parts) >= 2 {
		c.params[parts[0]] = parts[1]
	}
}

// readMsg reads one backend message.
func (c *Conn) readMsg() (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		c.broken = true
		return 0, nil, err
	}
	n := int(binary.BigEndian.Uint32(hdr[1:])) - 4
	if n < 0 || n > 1<<30 {
		c.broken = true
		return 0, nil, fmt.Errorf("bad message length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c.r, body); err != nil {
		c.broken = true
		return 0, nil, err
	}
	return hdr[0], body, nil
}

func parseError(msg []byte) *PgError {
	e := &PgError{}
	for len(msg) > 0 && msg[0] != 0 {
		field := msg[0]
		end := 1
		for end < len(msg) && msg[end] != 0 {
			end++
		}
		val := string(msg[1:end])
		switch field {
		case 'S':
			if e.Severity == "" {
				e.Severity = val
			}
		case 'V':
			e.Severity = val
		case 'C':
			e.Code = val
		case 'M':
			e.Message = val
		case 'D':
			e.Detail = val
		case 'H':
			e.Hint = val
		case 'P':
			e.Position = val
		}
		if end >= len(msg) {
			break
		}
		msg = msg[end+1:]
	}
	return e
}

// Exec runs sql with the simple query protocol and returns one Result per
// statement. If a statement fails, Exec returns the server's error after
// reading the rest of the reply, so the connection stays usable.
//
// When ctx ends first, Exec asks the server to cancel the statement and waits
// a few seconds for it to stop; if it doesn't, the connection is closed.
func (c *Conn) Exec(ctx context.Context, sql string) ([]Result, error) {
	if c.broken {
		return nil, errors.New("the connection is broken")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	msg := make([]byte, 5, 6+len(sql))
	msg[0] = 'Q'
	msg = append(msg, sql...)
	msg = append(msg, 0)
	binary.BigEndian.PutUint32(msg[1:], uint32(len(msg)-1))

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		c.cancelRequest()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			c.netConn.SetDeadline(time.Unix(1, 0))
		}
	}()
	defer func() {
		close(done)
		wg.Wait()
	}()

	if _, err := c.netConn.Write(msg); err != nil {
		c.broken = true
		return nil, err
	}
	var results []Result
	var cur *Result
	var firstErr error
	for {
		typ, body, err := c.readMsg()
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%w (the connection was closed)", ctx.Err())
			}
			return nil, err
		}
		switch typ {
		case 'T':
			cur = &Result{Columns: parseRowDescription(body)}
		case 'D':
			if cur == nil {
				cur = &Result{}
			}
			row, err := parseDataRow(body)
			if err != nil {
				c.broken = true
				return nil, err
			}
			cur.Rows = append(cur.Rows, row)
		case 'C':
			if cur == nil {
				cur = &Result{}
			}
			cur.Tag = strings.TrimRight(string(body), "\x00")
			results = append(results, *cur)
			cur = nil
		case 'I':
			cur = nil
		case 'E':
			if firstErr == nil {
				firstErr = parseError(body)
			}
			cur = nil
		case 'N':
			c.Notices = append(c.Notices, parseError(body).Message)
		case 'S':
			c.paramStatus(body)
		case 'A':
		case 'G':
			// COPY FROM STDIN: refuse it.
			fail := []byte{'f', 0, 0, 0, 0}
			fail = append(fail, "LQO doesn't send COPY data"...)
			fail = append(fail, 0)
			binary.BigEndian.PutUint32(fail[1:], uint32(len(fail)-1))
			if _, err := c.netConn.Write(fail); err != nil {
				c.broken = true
				return nil, err
			}
		case 'H', 'd', 'c', 'W':
			// COPY TO STDOUT: read and drop the data.
		case 'Z':
			if len(body) > 0 {
				c.txStatus = body[0]
			}
			if firstErr != nil {
				return results, firstErr
			}
			if ctx.Err() != nil {
				return results, ctx.Err()
			}
			return results, nil
		default:
			c.broken = true
			return nil, fmt.Errorf("unexpected message %q", typ)
		}
	}
}

func parseRowDescription(b []byte) []string {
	if len(b) < 2 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	cols := make([]string, 0, n)
	for i := 0; i < n && len(b) > 0; i++ {
		end := 0
		for end < len(b) && b[end] != 0 {
			end++
		}
		cols = append(cols, string(b[:end]))
		if end+1+18 > len(b) {
			break
		}
		b = b[end+1+18:]
	}
	return cols
}

func parseDataRow(b []byte) ([]*string, error) {
	if len(b) < 2 {
		return nil, errors.New("short data row")
	}
	n := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	row := make([]*string, n)
	for i := 0; i < n; i++ {
		if len(b) < 4 {
			return nil, errors.New("short data row")
		}
		l := int32(binary.BigEndian.Uint32(b))
		b = b[4:]
		if l < 0 {
			continue
		}
		if int(l) > len(b) {
			return nil, errors.New("short data row")
		}
		s := string(b[:l])
		row[i] = &s
		b = b[l:]
	}
	return row, nil
}

// cancelRequest asks the server, on a second connection, to stop whatever
// this connection is running. It is best effort.
func (c *Conn) cancelRequest() {
	if len(c.secret) == 0 {
		return
	}
	network, addr := c.cfg.Address()
	nc, err := net.DialTimeout(network, addr, 5*time.Second)
	if err != nil {
		return
	}
	defer nc.Close()
	nc.SetDeadline(time.Now().Add(5 * time.Second))
	var conn net.Conn = nc
	if c.usedTLS {
		req := make([]byte, 8)
		binary.BigEndian.PutUint32(req[0:], 8)
		binary.BigEndian.PutUint32(req[4:], 80877103)
		if _, err := nc.Write(req); err != nil {
			return
		}
		ans := make([]byte, 1)
		if _, err := io.ReadFull(nc, ans); err != nil {
			return
		}
		if ans[0] == 'S' {
			tc := tls.Client(nc, c.tlsConf)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn = tc
		}
	}
	b := make([]byte, 0, 12+len(c.secret))
	b = binary.BigEndian.AppendUint32(b, uint32(12+len(c.secret)))
	b = binary.BigEndian.AppendUint32(b, 80877102)
	b = binary.BigEndian.AppendUint32(b, c.pid)
	b = append(b, c.secret...)
	conn.Write(b)
	io.Copy(io.Discard, conn) // the server closes the socket when done
}

// QueryValue runs sql and returns the first column of the first row.
func (c *Conn) QueryValue(ctx context.Context, sql string) (string, error) {
	res, err := c.Exec(ctx, sql)
	if err != nil {
		return "", err
	}
	for _, r := range res {
		if len(r.Rows) > 0 && len(r.Rows[0]) > 0 {
			if r.Rows[0][0] == nil {
				return "", nil
			}
			return *r.Rows[0][0], nil
		}
	}
	return "", errors.New("the query returned no rows")
}

// ServerVersionNum is the server version as a number, such as 160004 for 16.4.
func (c *Conn) ServerVersionNum() int {
	v := c.params["server_version"]
	fields := strings.FieldsFunc(v, func(r rune) bool { return r != '.' && (r < '0' || r > '9') })
	if len(fields) == 0 {
		return 0
	}
	parts := strings.Split(fields[0], ".")
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if major >= 10 {
		return major*10000 + minor
	}
	return major*10000 + minor*100
}

// Param is a run-time setting the server reported, such as server_version.
func (c *Conn) Param(name string) string { return c.params[name] }

// TxStatus is I when idle, T inside a transaction, E inside a failed one.
func (c *Conn) TxStatus() byte { return c.txStatus }

// Broken reports whether the connection can no longer be used.
func (c *Conn) Broken() bool { return c.broken }

// UsedTLS reports whether the connection is encrypted.
func (c *Conn) UsedTLS() bool { return c.usedTLS }

// Close says goodbye and closes the socket.
func (c *Conn) Close() error {
	if c.netConn == nil {
		return nil
	}
	if !c.broken {
		c.netConn.SetDeadline(time.Now().Add(time.Second))
		c.netConn.Write([]byte{'X', 0, 0, 0, 4})
	}
	err := c.netConn.Close()
	c.netConn = nil
	return err
}
