package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestUpstreamDoRewritesSchemeAndHost(t *testing.T) {
	var gotPath, gotHost string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer fake.Close()

	base, err := url.Parse(fake.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	up := NewUpstream(base, nil)

	// Simulate the engine building a request from an incoming server
	// request: URL carries only path/query, no scheme or host.
	req, err := http.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := up.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("upstream saw path %q, want /v1/chat/completions", gotPath)
	}
	if gotHost != base.Host {
		t.Fatalf("upstream saw Host %q, want %q", gotHost, base.Host)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}
}

func TestUpstreamDoPropagatesUpstreamError(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:0") // nothing listens here
	up := NewUpstream(base, nil)

	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	if _, err := up.Do(req); err == nil {
		t.Fatal("Do: want error when upstream is unreachable, got nil")
	}
}
