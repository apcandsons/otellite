package client_test

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apcandsons/otellite/client"
	"github.com/apcandsons/otellite/client/clienttest"
)

func TestHTTPMiddlewareCountsRequestsErrorsAndBytes(t *testing.T) {
	iamEnv(t)
	rcv := clienttest.NewReceiver(t)
	start(t, rcv, client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})

	h := client.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("hello"))
		case "/boom":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	get := func(path string, want int) {
		t.Helper()
		resp, err := http.Post(srv.URL+path, "text/plain", strings.NewReader("abc"))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s: status %d, want %d", path, resp.StatusCode, want)
		}
	}
	get("/ok", 200)
	get("/ok", 200)
	get("/boom", 500)
	get("/missing", 404)

	const m = "/iam/iam-api/metrics/http.server."
	if got := waitSum(t, rcv, m+"requests.dat", 4); got != 4 {
		t.Fatalf("requests = %v", got)
	}
	if got := waitSum(t, rcv, m+"errors.dat", 1); got != 1 {
		t.Fatalf("errors = %v (only 5xx count)", got)
	}
	if got := waitSum(t, rcv, m+"duration.count.dat", 4); got != 4 {
		t.Fatalf("duration.count = %v", got)
	}
	if s := rcv.Samples(m + "duration.sum.dat"); len(s) == 0 || s[0].Unit != "ms" {
		t.Fatalf("duration unit: %+v", s)
	}
	if got := waitSum(t, rcv, m+"bytes_in.dat", 12); got != 12 {
		t.Fatalf("bytes_in = %v", got)
	}
	if got := waitSum(t, rcv, m+"bytes_out.dat", 10); got != 10 {
		t.Fatalf("bytes_out = %v", got)
	}
	s := rcv.Wait(m+"active_requests.dat", 1, waitFor)
	if len(s) == 0 || s[len(s)-1].Value != "0" {
		t.Fatalf("active_requests should settle at 0: %+v", s)
	}
}

// flusher is a ResponseWriter that only implements Flusher, like
// httptest.ResponseRecorder and the real server's writer.
type hijackable struct {
	http.ResponseWriter
	hijacked bool
}

func (h *hijackable) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func TestHTTPMiddlewareKeepsFlusherHijackerAndUnwrap(t *testing.T) {
	iamEnv(t)
	shutdown, err := client.Start(context.Background(), client.Options{Handler: slog.NewTextHandler(&buffer{}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())

	rec := httptest.NewRecorder()
	var sawFlusher, flushed bool
	var unwrapped http.ResponseWriter
	h := client.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawFlusher = w.(http.Flusher)
		flushed = http.NewResponseController(w).Flush() == nil
		unwrapped = w.(interface{ Unwrap() http.ResponseWriter }).Unwrap()
		w.Write([]byte("data: x\n\n"))
	}))
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))
	if !sawFlusher || !flushed || !rec.Flushed {
		t.Fatalf("SSE broken: flusher=%v flushed=%v recorder=%v", sawFlusher, flushed, rec.Flushed)
	}
	if unwrapped != rec {
		t.Fatalf("Unwrap() = %T, want the recorder", unwrapped)
	}

	hj := &hijackable{ResponseWriter: httptest.NewRecorder()}
	h = client.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := w.(http.Hijacker).Hijack(); err != nil {
			t.Errorf("Hijack: %v", err)
		}
	}))
	h.ServeHTTP(hj, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if !hj.hijacked {
		t.Fatal("Hijack did not pass through")
	}

	// No Hijacker underneath: report ErrNotSupported instead of panicking.
	h = client.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := w.(http.Hijacker).Hijack(); err == nil {
			t.Error("Hijack should fail when the underlying writer cannot")
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ws", nil))
}
