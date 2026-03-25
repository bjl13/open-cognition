// Package pg provides a minimal PostgreSQL wire-protocol v3 client.
//
// Design constraints:
//   - Zero external dependencies (stdlib only)
//   - Supports: cleartext + MD5 authentication, simple query protocol
//   - Not production-hardened; suited for the reference implementation
//
// The server must be configured for MD5 or trust authentication.
// SCRAM-SHA-256 is not supported in this minimal implementation.
package pg

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// DSN parsing
// ---------------------------------------------------------------------------

type config struct {
	host     string
	port     string
	user     string
	password string
	database string
}

func parseDSN(dsn string) (config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return config{}, fmt.Errorf("pg: parse DSN: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	user := u.User.Username()
	password, _ := u.User.Password()
	database := strings.TrimPrefix(u.Path, "/")
	if database == "" {
		database = user
	}
	return config{host: host, port: port, user: user, password: password, database: database}, nil
}

// ---------------------------------------------------------------------------
// Low-level connection
// ---------------------------------------------------------------------------

type conn struct {
	nc net.Conn
}

func dial(ctx context.Context, cfg config) (*conn, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "tcp", net.JoinHostPort(cfg.host, cfg.port))
	if err != nil {
		return nil, fmt.Errorf("pg: dial %s:%s: %w", cfg.host, cfg.port, err)
	}
	c := &conn{nc: nc}
	if err := c.handshake(cfg); err != nil {
		_ = nc.Close()
		return nil, err
	}
	return c, nil
}

// handshake sends the startup message and handles the authentication exchange.
func (c *conn) handshake(cfg config) error {
	// Build startup message.
	// Format: [int32 length][int32 proto=196608][key\0val\0...][0]
	var params []byte
	for _, kv := range [][2]string{
		{"user", cfg.user},
		{"database", cfg.database},
		{"application_name", "open-cognition"},
		{"client_encoding", "UTF8"},
	} {
		params = append(params, kv[0]...)
		params = append(params, 0)
		params = append(params, kv[1]...)
		params = append(params, 0)
	}
	params = append(params, 0) // terminator

	buf := make([]byte, 8+len(params))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(buf)))
	binary.BigEndian.PutUint32(buf[4:8], 196608) // version 3.0
	copy(buf[8:], params)

	if _, err := c.nc.Write(buf); err != nil {
		return fmt.Errorf("pg: send startup: %w", err)
	}
	return c.authenticate(cfg)
}

func (c *conn) authenticate(cfg config) error {
	for {
		msgType, body, err := c.readMsg()
		if err != nil {
			return err
		}
		switch msgType {
		case 'R': // Authentication request
			if len(body) < 4 {
				return fmt.Errorf("pg: malformed auth message")
			}
			authType := binary.BigEndian.Uint32(body[0:4])
			switch authType {
			case 0: // AuthenticationOK
				return c.drainUntilReady()
			case 3: // Cleartext password
				if err := c.sendPassword(cfg.password); err != nil {
					return err
				}
			case 5: // MD5 password
				if len(body) < 8 {
					return fmt.Errorf("pg: malformed MD5 auth")
				}
				hash := md5Password(cfg.user, cfg.password, body[4:8])
				if err := c.sendPassword(hash); err != nil {
					return err
				}
			default:
				return fmt.Errorf("pg: unsupported auth type %d (server may require SCRAM-SHA-256; set POSTGRES_HOST_AUTH_METHOD=md5)", authType)
			}
		case 'E':
			return pgError(body)
		}
	}
}

func md5Password(user, password string, salt []byte) string {
	h1 := md5.Sum([]byte(password + user))
	hex1 := fmt.Sprintf("%x", h1)
	h2data := make([]byte, len(hex1)+len(salt))
	copy(h2data, hex1)
	copy(h2data[len(hex1):], salt)
	h2 := md5.Sum(h2data)
	return "md5" + fmt.Sprintf("%x", h2)
}

func (c *conn) sendPassword(pw string) error {
	// 'p' + int32(len) + pw + null
	msg := make([]byte, 1+4+len(pw)+1)
	msg[0] = 'p'
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(pw)+1))
	copy(msg[5:], pw)
	_, err := c.nc.Write(msg)
	return err
}

// drainUntilReady reads and discards messages until ReadyForQuery.
func (c *conn) drainUntilReady() error {
	for {
		msgType, body, err := c.readMsg()
		if err != nil {
			return err
		}
		switch msgType {
		case 'Z': // ReadyForQuery
			return nil
		case 'E':
			return pgError(body)
		// 'S' ParameterStatus, 'K' BackendKeyData, 'N' NoticeResponse — ignore
		}
	}
}

// ---------------------------------------------------------------------------
// Wire protocol I/O
// ---------------------------------------------------------------------------

func (c *conn) readMsg() (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(c.nc, header); err != nil {
		return 0, nil, fmt.Errorf("pg: read header: %w", err)
	}
	msgType := header[0]
	length := binary.BigEndian.Uint32(header[1:5])
	if length < 4 {
		return 0, nil, fmt.Errorf("pg: invalid message length %d", length)
	}
	body := make([]byte, length-4)
	if len(body) > 0 {
		if _, err := io.ReadFull(c.nc, body); err != nil {
			return 0, nil, fmt.Errorf("pg: read body: %w", err)
		}
	}
	return msgType, body, nil
}

func pgError(body []byte) error {
	var severity, message, detail string
	for i := 0; i < len(body); {
		code := body[i]
		i++
		if code == 0 {
			break
		}
		end := i
		for end < len(body) && body[end] != 0 {
			end++
		}
		val := string(body[i:end])
		i = end + 1
		switch code {
		case 'S':
			severity = val
		case 'M':
			message = val
		case 'D':
			detail = val
		}
	}
	if detail != "" {
		return fmt.Errorf("pg %s: %s (%s)", severity, message, detail)
	}
	return fmt.Errorf("pg %s: %s", severity, message)
}

// ---------------------------------------------------------------------------
// Query execution
// ---------------------------------------------------------------------------

// rows is a parsed result set: [][]string (NULL represented as empty string).
type rows [][]string

func (c *conn) exec(ctx context.Context, sql string) error {
	_, err := c.query(ctx, sql)
	return err
}

func (c *conn) query(ctx context.Context, sql string) (rows, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.nc.SetDeadline(deadline)
		defer c.nc.SetDeadline(time.Time{}) //nolint:errcheck
	}

	// Simple Query message: 'Q' + int32(length) + query + null
	q := sql + "\x00"
	msg := make([]byte, 5+len(q))
	msg[0] = 'Q'
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(q)))
	copy(msg[5:], q)

	if _, err := c.nc.Write(msg); err != nil {
		return nil, fmt.Errorf("pg: send query: %w", err)
	}

	var ncols int
	var result rows
	for {
		msgType, body, err := c.readMsg()
		if err != nil {
			return nil, err
		}
		switch msgType {
		case 'T': // RowDescription
			ncols = parseRowDesc(body)
		case 'D': // DataRow
			result = append(result, parseDataRow(body, ncols))
		case 'C': // CommandComplete — keep going until ReadyForQuery
		case 'Z': // ReadyForQuery
			return result, nil
		case 'E': // ErrorResponse
			return nil, pgError(body)
		case 'I': // EmptyQueryResponse
		case 'N': // NoticeResponse — ignore
		}
	}
}

func parseRowDesc(body []byte) int {
	if len(body) < 2 {
		return 0
	}
	return int(binary.BigEndian.Uint16(body[0:2]))
}

func parseDataRow(body []byte, ncols int) []string {
	if len(body) < 2 {
		return nil
	}
	count := int(binary.BigEndian.Uint16(body[0:2]))
	if ncols > 0 {
		count = ncols
	}
	row := make([]string, count)
	pos := 2
	for i := 0; i < count; i++ {
		if pos+4 > len(body) {
			break
		}
		length := int(int32(binary.BigEndian.Uint32(body[pos : pos+4])))
		pos += 4
		if length < 0 {
			row[i] = "" // NULL
		} else if pos+length <= len(body) {
			row[i] = string(body[pos : pos+length])
			pos += length
		}
	}
	return row
}

func (c *conn) close() {
	_, _ = c.nc.Write([]byte{'X', 0, 0, 0, 4})
	_ = c.nc.Close()
}

// ---------------------------------------------------------------------------
// Value scanning
// ---------------------------------------------------------------------------

func scanRow(row []string, dest ...interface{}) error {
	if len(row) < len(dest) {
		return fmt.Errorf("pg scan: have %d columns, want %d", len(row), len(dest))
	}
	for i, d := range dest {
		if err := scanValue(row[i], d); err != nil {
			return fmt.Errorf("pg scan col %d: %w", i, err)
		}
	}
	return nil
}

func scanValue(s string, dest interface{}) error {
	switch d := dest.(type) {
	case *string:
		*d = s
	case *bool:
		*d = s == "t" || s == "true" || s == "1"
	case *int:
		n, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*d = n
	case *int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*d = n
	case *time.Time:
		// PostgreSQL returns TIMESTAMPTZ in several formats depending on DateStyle.
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05-07:00",
			"2006-01-02 15:04:05.999999999+00",
			"2006-01-02 15:04:05+00",
		} {
			if t, err := time.Parse(layout, s); err == nil {
				*d = t.UTC()
				return nil
			}
		}
		return fmt.Errorf("cannot parse time %q", s)
	default:
		return fmt.Errorf("unsupported scan destination %T", dest)
	}
	return nil
}

// ---------------------------------------------------------------------------
// SQL value quoting (simple query protocol — no server-side parameters)
// ---------------------------------------------------------------------------
// PostgreSQL uses standard_conforming_strings=on by default since 9.1,
// so backslash is treated literally and only single-quotes need escaping.

// QuoteLiteral escapes s for use as a SQL string literal.
func QuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// QuoteLiteralOrNULL returns NULL for a nil pointer, or QuoteLiteral for non-nil.
func QuoteLiteralOrNULL(s *string) string {
	if s == nil {
		return "NULL"
	}
	return QuoteLiteral(*s)
}

// FormatFloat returns the float as a SQL numeric literal, or NULL for nil.
func FormatFloat(f *float64) string {
	if f == nil {
		return "NULL"
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

// FormatJSONOrNULL serialises raw JSON bytes as a quoted SQL literal,
// or NULL if b is nil.
func FormatJSONOrNULL(b []byte) string {
	if b == nil {
		return "NULL"
	}
	return QuoteLiteral(string(b))
}

// ---------------------------------------------------------------------------
// Connection pool
// ---------------------------------------------------------------------------

// Pool is a simple fixed-size connection pool.
type Pool struct {
	cfg   config
	conns chan *conn
	mu    sync.Mutex
}

// NewPool creates a pool and verifies connectivity with one initial connection.
func NewPool(ctx context.Context, dsn string, maxConns int) (*Pool, error) {
	cfg, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	if maxConns <= 0 {
		maxConns = 4
	}
	p := &Pool{
		cfg:   cfg,
		conns: make(chan *conn, maxConns),
	}
	// Validate connectivity.
	c, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	p.conns <- c
	return p, nil
}

func (p *Pool) acquire(ctx context.Context) (*conn, error) {
	select {
	case c := <-p.conns:
		return c, nil
	default:
		return dial(ctx, p.cfg)
	}
}

func (p *Pool) release(c *conn) {
	select {
	case p.conns <- c:
	default:
		c.close() // pool full — discard
	}
}

// Exec acquires a connection, runs sql (no result), and returns it to the pool.
// On error the connection is discarded.
func (p *Pool) Exec(ctx context.Context, sql string) error {
	c, err := p.acquire(ctx)
	if err != nil {
		return err
	}
	if err := c.exec(ctx, sql); err != nil {
		c.close()
		return err
	}
	p.release(c)
	return nil
}

// Query acquires a connection, runs sql, and returns all rows.
func (p *Pool) Query(ctx context.Context, sql string) (rows, error) {
	c, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}
	r, err := c.query(ctx, sql)
	if err != nil {
		c.close()
		return nil, err
	}
	p.release(c)
	return r, nil
}

// QueryRow runs sql and scans the first returned row into dest.
// Returns an error if no rows are returned.
func (p *Pool) QueryRow(ctx context.Context, sql string, dest ...interface{}) error {
	r, err := p.Query(ctx, sql)
	if err != nil {
		return err
	}
	if len(r) == 0 {
		return fmt.Errorf("pg: query returned no rows")
	}
	return scanRow(r[0], dest...)
}

// Close terminates all idle connections.
func (p *Pool) Close() {
	close(p.conns)
	for c := range p.conns {
		c.close()
	}
}
