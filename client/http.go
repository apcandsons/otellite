package client

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/metric"
)

type httpInstruments struct {
	requests, errors, bytesIn, bytesOut metric.Int64Counter
	duration                            metric.Float64Histogram
	active                              metric.Int64UpDownCounter
}

var httpInst lazy[*httpInstruments]

func newHTTPInstruments() *httpInstruments {
	return &httpInstruments{
		requests: Counter("http.server.requests", "1", "HTTP requests completed"),
		errors:   Counter("http.server.errors", "1", "HTTP responses with status >= 500"),
		bytesIn:  Counter("http.server.bytes_in", "By", "Request body bytes read"),
		bytesOut: Counter("http.server.bytes_out", "By", "Response body bytes written"),
		duration: Histogram("http.server.duration", "ms", "HTTP request latency"),
		active:   UpDown("http.server.active_requests", "1", "In-flight HTTP requests"),
	}
}

// HTTPMiddleware instruments an http.Handler with http.server.{requests,
// errors, duration, active_requests, bytes_in, bytes_out}. The wrapped
// ResponseWriter keeps Flusher and Hijacker working (and implements
// Unwrap for http.ResponseController), so SSE and WebSockets are fine.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		in := httpInst.get(newHTTPInstruments)
		ctx := r.Context()
		start := time.Now()
		in.active.Add(ctx, 1)

		body := &countingReader{ReadCloser: r.Body}
		if r.Body != nil {
			r.Body = body
		}
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			in.active.Add(ctx, -1)
			in.requests.Add(ctx, 1)
			if rw.status >= http.StatusInternalServerError {
				in.errors.Add(ctx, 1)
			}
			in.duration.Record(ctx, millis(time.Since(start)))
			in.bytesIn.Add(ctx, body.n)
			in.bytesOut.Add(ctx, rw.bytes)
		}()
		next.ServeHTTP(rw, r)
	})
}

type countingReader struct {
	io.ReadCloser
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.n += int64(n)
	return n, err
}

// responseWriter records the status and byte count while passing every
// optional interface through to the real writer.
type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.wrote = true
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) Flush() { _ = w.FlushError() }

func (w *responseWriter) FlushError() error {
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
