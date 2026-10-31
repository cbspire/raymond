package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cbspire/raymond/internal/install"
)

// ParseRequestLog projects only the proxy's safe access fields. Arbitrary service
// errors, native tool output, credentials and payloads never reach a log client.
func ParseRequestLog(line string) map[string]any {
	if i := strings.Index(line, "msg=request "); i >= 0 {
		line = line[i:]
	}
	fields := map[string]string{}
	if i := strings.Index(line, "INFO request "); i >= 0 {
		line = line[i+len("INFO request "):]
		fields["msg"] = "request"
	}
	for len(line) > 0 {
		line = strings.TrimSpace(line)
		key, rest, ok := strings.Cut(line, "=")
		if !ok || strings.ContainsAny(key, " \t") {
			break
		}
		value := ""
		if strings.HasPrefix(rest, "\"") {
			end := 1
			for end < len(rest) {
				if rest[end] == '\\' {
					end += 2
					continue
				}
				if rest[end] == '"' {
					break
				}
				end++
			}
			if end >= len(rest) {
				return nil
			}
			value, _ = strconv.Unquote(rest[:end+1])
			line = rest[end+1:]
		} else {
			value, line, _ = strings.Cut(rest, " ")
		}
		fields[key] = value
	}
	if fields["msg"] != "request" {
		return nil
	}
	code, e := strconv.Atoi(fields["status"])
	if e != nil || code < 100 || code > 599 {
		return nil
	}
	host := fields["host"]
	if strings.Contains(host, ":") {
		var port string
		var err error
		host, port, err = net.SplitHostPort(host)
		n, e := strconv.Atoi(port)
		if err != nil || e != nil || n < 1 || n > 65535 {
			return nil
		}
	}
	if _, e := config.Normalize(host); e != nil {
		return nil
	}
	if !strings.HasPrefix(fields["path"], "/") {
		return nil
	}
	result := map[string]any{"status": code}
	for _, key := range []string{"method", "host", "path"} {
		value := fields[key]
		if len(value) > 2048 || strings.ContainsAny(value, "\r\n\x00") {
			return nil
		}
		if key == "path" {
			value, _, _ = strings.Cut(value, "?")
		}
		result[key] = value
	}
	for _, key := range []string{"io", "duration_ms"} {
		if n, e := strconv.ParseFloat(fields[key], 64); e == nil && n >= 0 && !math.IsInf(n, 0) && !math.IsNaN(n) {
			result[key] = n
		}
	}
	return result
}

type logWriter struct {
	ctx     context.Context
	cancel  context.CancelFunc
	w       http.ResponseWriter
	mu      sync.Mutex
	pending []byte
	discard bool
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	for _, c := range p {
		if c != '\n' {
			if len(l.pending) < 8192 && !l.discard {
				l.pending = append(l.pending, c)
			} else {
				l.discard = true
			}
			continue
		}
		if !l.discard {
			if entry := ParseRequestLog(string(l.pending)); entry != nil {
				_ = http.NewResponseController(l.w).SetWriteDeadline(time.Now().Add(5 * time.Second))
				if e := json.NewEncoder(l.w).Encode(entry); e != nil {
					l.cancel()
					return 0, e
				}
				if e := http.NewResponseController(l.w).Flush(); e != nil {
					l.cancel()
					return 0, e
				}
			}
		}
		l.pending = l.pending[:0]
		l.discard = false
	}
	select {
	case <-l.ctx.Done():
		return 0, l.ctx.Err()
	default:
		return n, nil
	}
}
func (b *Backend) Logs(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	for key, values := range r.URL.Query() {
		if len(values) != 1 {
			http.Error(w, "ambiguous log option", 400)
			return
		}
		if key != "lines" && key != "follow" {
			http.Error(w, "unknown log option", 400)
			return
		}
	}
	options := install.LogOptions{Lines: 50, Follow: true}
	var e error
	if s := r.URL.Query().Get("lines"); s != "" {
		options.Lines, e = strconv.Atoi(s)
		if e != nil {
			http.Error(w, "invalid line count", 400)
			return
		}
	}
	if s := r.URL.Query().Get("follow"); s != "" {
		options.Follow, e = strconv.ParseBool(s)
		if e != nil {
			http.Error(w, "invalid follow option", 400)
			return
		}
	}
	if e = options.Validate(); e != nil {
		http.Error(w, "invalid log options", 400)
		return
	}
	select {
	case b.streams <- struct{}{}:
		defer func() { <-b.streams }()
	default:
		http.Error(w, "too many log streams", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	_ = http.NewResponseController(w).Flush()
	output := &logWriter{ctx: ctx, cancel: cancel, w: w}
	errors := &limitedBuffer{limit: 8192}
	// Native stderr is deliberately not forwarded to a browser.
	if e = install.StreamLogs(ctx, options, output, errors); e != nil && ctx.Err() == nil {
		_, _ = fmt.Fprintln(w, `{"event":"disconnected"}`)
	}
}
