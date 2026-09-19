package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeHandler struct {
	resp *http.Response
	err  error
}

func (h *fakeHandler) Handle(ctx context.Context, req *http.Request) (*http.Response, error) {
	return h.resp, h.err
}

func TestProxyServeHTTPForwardsResponse(t *testing.T) {
	body := `{"choices":[]}`
	handler := &fakeHandler{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": {"application/json"}, "Content-Length": {"9999"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}}

	srv := httptest.NewServer(&Proxy{Handler: handler})
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	// The handler's stale Content-Length (9999, left over from an upstream
	// that doesn't match the buffered body) must never reach the client:
	// Proxy recomputes it from what it actually wrote.
	if resp.ContentLength != int64(len(body)) {
		t.Fatalf("ContentLength = %d, want %d (the stale value would be 9999)", resp.ContentLength, len(body))
	}
}

func TestProxyServeHTTPReturns502OnHandlerError(t *testing.T) {
	handler := &fakeHandler{err: errors.New("upstream unreachable")}

	var loggedFmt string
	srv := httptest.NewServer(&Proxy{
		Handler:  handler,
		ErrorLog: func(format string, args ...any) { loggedFmt = format },
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	if loggedFmt == "" {
		t.Fatal("ErrorLog was never called")
	}
}

type fakeMissError struct{ msg string }

func (e *fakeMissError) Error() string      { return e.msg }
func (e *fakeMissError) CassetteMiss() bool { return true }

func TestProxyServeHTTPSetsMissHeaderOnCassetteMiss(t *testing.T) {
	handler := &fakeHandler{err: &fakeMissError{msg: "cassette miss"}}

	srv := httptest.NewServer(&Proxy{Handler: handler, ErrorLog: func(string, ...any) {}})
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	if resp.Header.Get(MissHeader) != "1" {
		t.Fatalf("%s header = %q, want %q", MissHeader, resp.Header.Get(MissHeader), "1")
	}
}

func TestProxyServeHTTPDoesNotSetMissHeaderOnGenericError(t *testing.T) {
	handler := &fakeHandler{err: errors.New("network blip")}

	srv := httptest.NewServer(&Proxy{Handler: handler, ErrorLog: func(string, ...any) {}})
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get(MissHeader) != "" {
		t.Fatalf("%s header should not be set for a generic error, got %q", MissHeader, resp.Header.Get(MissHeader))
	}
}
