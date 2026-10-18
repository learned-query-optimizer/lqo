// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package pgwire is a small PostgreSQL client that speaks the simple query
// protocol and nothing else. It is all LQO needs: it sends SQL as text and
// reads rows back as text. It uses only the Go standard library.
package pgwire

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is a parsed connection string.
type Config struct {
	Host            string // a host name, an IP address, or a directory holding a Unix socket
	Port            int
	User            string
	Password        string
	Database        string
	SSLMode         string // disable, allow, prefer, require, verify-ca, verify-full
	SSLRootCert     string
	SSLCert         string
	SSLKey          string
	ApplicationName string
	ConnectTimeout  time.Duration
	Options         string
	PassFile        string
}

// IsUnixSocket reports whether Host names a socket directory.
func (c *Config) IsUnixSocket() bool { return strings.HasPrefix(c.Host, "/") }

// Address is the network and address to dial.
func (c *Config) Address() (network, addr string) {
	if c.IsUnixSocket() {
		return "unix", filepath.Join(c.Host, fmt.Sprintf(".s.PGSQL.%d", c.Port))
	}
	host := c.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return "tcp", fmt.Sprintf("%s:%d", host, c.Port)
}

// Describe is the connection target without the password, for messages.
func (c *Config) Describe() string {
	_, addr := c.Address()
	return fmt.Sprintf("%s as %s, database %s", addr, c.User, c.Database)
}

// ParseConfig reads a connection string in either form libpq accepts: a URL
// (postgres://user:password@host:5432/db?sslmode=require) or key=value pairs
// (host=db1 port=5432 user=app dbname=shop). Settings the string leaves out
// come from the usual PG* environment variables, then from defaults.
func ParseConfig(dsn string) (*Config, error) {
	kv := map[string]string{}
	dsn = strings.TrimSpace(dsn)
	var err error
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		err = parseURL(dsn, kv)
	} else if dsn != "" {
		err = parseKeywords(dsn, kv)
	}
	if err != nil {
		return nil, err
	}
	env := func(key, name string) {
		if _, ok := kv[key]; !ok {
			if v := os.Getenv(name); v != "" {
				kv[key] = v
			}
		}
	}
	env("host", "PGHOST")
	env("port", "PGPORT")
	env("user", "PGUSER")
	env("password", "PGPASSWORD")
	env("dbname", "PGDATABASE")
	env("sslmode", "PGSSLMODE")
	env("sslrootcert", "PGSSLROOTCERT")
	env("sslcert", "PGSSLCERT")
	env("sslkey", "PGSSLKEY")
	env("application_name", "PGAPPNAME")
	env("connect_timeout", "PGCONNECT_TIMEOUT")
	env("options", "PGOPTIONS")
	env("passfile", "PGPASSFILE")

	c := &Config{
		Host:            kv["host"],
		User:            kv["user"],
		Password:        kv["password"],
		Database:        kv["dbname"],
		SSLMode:         kv["sslmode"],
		SSLRootCert:     kv["sslrootcert"],
		SSLCert:         kv["sslcert"],
		SSLKey:          kv["sslkey"],
		ApplicationName: kv["application_name"],
		Options:         kv["options"],
		PassFile:        kv["passfile"],
		Port:            5432,
		ConnectTimeout:  10 * time.Second,
	}
	if p := kv["port"]; p != "" {
		if i := strings.IndexByte(p, ','); i >= 0 {
			p = p[:i]
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return nil, fmt.Errorf("invalid port %q", kv["port"])
		}
		c.Port = n
	}
	if i := strings.IndexByte(c.Host, ','); i >= 0 {
		return nil, errors.New("more than one host in the connection string; give LQO one host")
	}
	if c.Host == "" {
		c.Host = defaultHost(c.Port)
	}
	if c.User == "" {
		if u, err := user.Current(); err == nil {
			c.User = u.Username
		} else {
			c.User = "postgres"
		}
	}
	if c.Database == "" {
		c.Database = c.User
	}
	switch c.SSLMode {
	case "":
		c.SSLMode = "prefer"
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return nil, fmt.Errorf("invalid sslmode %q", c.SSLMode)
	}
	if t := kv["connect_timeout"]; t != "" {
		n, err := strconv.Atoi(t)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid connect_timeout %q", t)
		}
		if n > 0 {
			if n < 2 {
				n = 2 // libpq treats anything under 2 seconds as 2
			}
			c.ConnectTimeout = time.Duration(n) * time.Second
		} else {
			c.ConnectTimeout = 0
		}
	}
	if c.Password == "" {
		c.Password = lookupPassFile(c)
	}
	return c, nil
}

func defaultHost(port int) string {
	for _, dir := range []string{"/var/run/postgresql", "/tmp"} {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf(".s.PGSQL.%d", port))); err == nil {
			return dir
		}
	}
	return "localhost"
}

var urlParams = map[string]string{
	"sslmode": "sslmode", "sslrootcert": "sslrootcert", "sslcert": "sslcert", "sslkey": "sslkey",
	"application_name": "application_name", "connect_timeout": "connect_timeout",
	"options": "options", "passfile": "passfile", "host": "host", "port": "port",
	"user": "user", "password": "password", "dbname": "dbname",
}

func parseURL(dsn string, kv map[string]string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid connection URL: %w", err)
	}
	if u.User != nil {
		kv["user"] = u.User.Username()
		if p, ok := u.User.Password(); ok {
			kv["password"] = p
		}
	}
	if h := u.Hostname(); h != "" {
		kv["host"] = h
	}
	if p := u.Port(); p != "" {
		kv["port"] = p
	}
	if strings.Contains(u.Host, ",") {
		return errors.New("more than one host in the connection string; give LQO one host")
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		kv["dbname"] = db
	}
	for k, vs := range u.Query() {
		key, ok := urlParams[k]
		if !ok {
			return fmt.Errorf("connection URL setting %q isn't supported", k)
		}
		if len(vs) > 0 {
			kv[key] = vs[len(vs)-1]
		}
	}
	return nil
}

func parseKeywords(dsn string, kv map[string]string) error {
	s := dsn
	for {
		s = strings.TrimLeft(s, " \t\n\r")
		if s == "" {
			return nil
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return fmt.Errorf("invalid connection string near %q", s)
		}
		key := strings.TrimSpace(s[:eq])
		s = strings.TrimLeft(s[eq+1:], " \t")
		var val strings.Builder
		if strings.HasPrefix(s, "'") {
			s = s[1:]
			closed := false
			for i := 0; i < len(s); i++ {
				ch := s[i]
				if ch == '\\' && i+1 < len(s) {
					val.WriteByte(s[i+1])
					i++
					continue
				}
				if ch == '\'' {
					s = s[i+1:]
					closed = true
					break
				}
				val.WriteByte(ch)
			}
			if !closed {
				return errors.New("unterminated quoted value in connection string")
			}
		} else {
			i := 0
			for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
				if s[i] == '\\' && i+1 < len(s) {
					val.WriteByte(s[i+1])
					i += 2
					continue
				}
				val.WriteByte(s[i])
				i++
			}
			s = s[i:]
		}
		k, ok := urlParams[key]
		if !ok {
			if key == "hostaddr" || key == "gssencmode" || key == "target_session_attrs" || key == "client_encoding" {
				continue
			}
			return fmt.Errorf("connection setting %q isn't supported", key)
		}
		kv[k] = val.String()
	}
}

// lookupPassFile finds a password in ~/.pgpass (or the passfile setting),
// as libpq does. The file is skipped unless only its owner can read it.
func lookupPassFile(c *Config) string {
	path := c.PassFile
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, ".pgpass")
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	host := c.Host
	if c.IsUnixSocket() {
		host = "localhost"
	}
	want := []string{host, strconv.Itoa(c.Port), c.Database, c.User}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		fields := splitPassLine(line)
		if len(fields) != 5 {
			continue
		}
		match := true
		for i, w := range want {
			if fields[i] != "*" && fields[i] != w {
				match = false
				break
			}
		}
		if match {
			return fields[4]
		}
	}
	return ""
}

func splitPassLine(line string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line):
			cur.WriteByte(line[i+1])
			i++
		case line[i] == ':' && len(out) < 4:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(out, cur.String())
}
