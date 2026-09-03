package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthURLFromListenAddress(t *testing.T) {
	cases := map[string]string{
		":4318":             "http://127.0.0.1:4318/fs/ls?path=/",
		"0.0.0.0:4318":      "http://127.0.0.1:4318/fs/ls?path=/",
		"[::]:4318":         "http://127.0.0.1:4318/fs/ls?path=/",
		"localhost:9999":    "http://localhost:9999/fs/ls?path=/",
		"10.0.0.5:4318":     "http://10.0.0.5:4318/fs/ls?path=/",
		"[::1]:4318":        "http://[::1]:4318/fs/ls?path=/",
		"sor.internal:4318": "http://sor.internal:4318/fs/ls?path=/",
	}
	for listen, want := range cases {
		got, err := healthURL(listen)
		if err != nil {
			t.Errorf("%q: %v", listen, err)
		} else if got != want {
			t.Errorf("%q: got %q, want %q", listen, got, want)
		}
	}
	for _, bad := range []string{"", "4318", "localhost"} {
		if _, err := healthURL(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestCheckHealthPassesOn200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fs/ls" || r.URL.Query().Get("path") != "/" {
			http.Error(w, "wrong probe", http.StatusNotFound)
			return
		}
		w.Write([]byte("iam/\n"))
	}))
	defer srv.Close()
	if err := checkHealth(srv.URL+"/fs/ls?path=/", 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestCheckHealthFailsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := checkHealth(srv.URL+"/fs/ls?path=/", 2*time.Second); err == nil {
		t.Fatal("expected an error for a 500")
	}
}

func TestCheckHealthFailsWhenUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/fs/ls?path=/"
	srv.Close()
	if err := checkHealth(url, 500*time.Millisecond); err == nil {
		t.Fatal("expected an error for a closed port")
	}
}
