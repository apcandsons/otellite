package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// healthPath is the probe: listing the root of the virtual filesystem
// proves the HTTP server, the browser use case and the store all answer.
const healthPath = "/fs/ls?path=/"

// healthURL turns the -listen address into the URL a co-located probe
// should hit. An unspecified host (":4318", "0.0.0.0:4318", "[::]:4318")
// becomes 127.0.0.1; an explicit host is kept.
func healthURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("-listen %q: %w", listen, err)
	}
	if port == "" {
		return "", fmt.Errorf("-listen %q: missing port", listen)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + healthPath, nil
}

// checkHealth GETs url and returns nil only on HTTP 200 within timeout.
// It is the whole of `sor -healthcheck`, so a distroless image needs no
// curl or shell for its container health command.
func checkHealth(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("GET " + url + ": " + resp.Status)
	}
	return nil
}
