package proxy

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type accessWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *accessWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *accessWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *accessWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, e := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, e
}

func (w *accessWriter) Flush() { _ = w.FlushError() }

func (w *accessWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *accessWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	connection, buffer, e := http.NewResponseController(w.ResponseWriter).Hijack()
	if e == nil && w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return connection, buffer, e
}

// Log response metadata only. Query strings, headers, cookies and bodies may
// contain credentials and must never become request log fields.
func accessLog(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &accessWriter{ResponseWriter: w}
		defer func() {
			logger.Info("request",
				"method", r.Method,
				"host", r.Host,
				"path", r.URL.EscapedPath(),
				"status", response.status,
				"bytes", response.bytes,
				"duration_ms", float64(time.Since(started).Microseconds())/1000,
			)
		}()
		next.ServeHTTP(response, r)
		if response.status == 0 {
			response.status = http.StatusOK
		}
	})
}
