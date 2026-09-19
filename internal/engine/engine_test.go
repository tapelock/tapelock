package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/tapelock/tapelock/internal/cassette"
	"github.com/tapelock/tapelock/internal/sanitize"
)

type fakeUpstream struct {
	called   bool
	lastReq  *http.Request
	lastBody []byte

	resp *http.Response
	err  error
}

func (f *fakeUpstream) Do(req *http.Request) (*http.Response, error) {
	f.called = true
	f.lastReq = req
	if req.Body != nil {
		f.lastBody, _ = io.ReadAll(req.Body)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func jsonUpstreamResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}, "X-Upstream": {"openai"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type fakeStore struct {
	appended []cassette.Interaction
	err      error
}

func (f *fakeStore) Append(it cassette.Interaction) error {
	if f.err != nil {
		return f.err
	}
	f.appended = append(f.appended, it)
	return nil
}

func newRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-secret")
	return req
}

var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestEngineHandleForwardsAndRecords(t *testing.T) {
	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	up := &fakeUpstream{resp: jsonUpstreamResponse(200, `{"id":"chatcmpl-1","choices":[]}`)}
	store := &fakeStore{}
	e := &Engine{Upstream: up, Store: store}

	resp, err := e.Handle(context.Background(), newRequest(t, body))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	defer resp.Body.Close()

	if !up.called {
		t.Fatal("upstream was never called")
	}
	if string(up.lastBody) != body {
		t.Fatalf("upstream received %q, want the original body %q", up.lastBody, body)
	}
	if up.lastReq.Header.Get("Authorization") != "Bearer sk-secret" {
		t.Fatal("upstream request lost the Authorization header")
	}

	if resp.StatusCode != 200 {
		t.Fatalf("response status = %d, want 200", resp.StatusCode)
	}
	respBody, _ := io.ReadAll(resp.Body)
	if string(respBody) != `{"id":"chatcmpl-1","choices":[]}` {
		t.Fatalf("response body = %q", respBody)
	}
	if resp.Header.Get("X-Upstream") != "openai" {
		t.Fatal("response lost the upstream's X-Upstream header")
	}

	if len(store.appended) != 1 {
		t.Fatalf("cassette has %d interactions, want 1", len(store.appended))
	}
	it := store.appended[0]
	if it.Request.Body != body {
		t.Fatalf("stored request body = %q, want the original %q", it.Request.Body, body)
	}
	if !hashPattern.MatchString(it.RequestHash) {
		t.Fatalf("RequestHash %q does not look like sha256:<hex>", it.RequestHash)
	}
	if it.Response.Status != 200 || it.Response.Body != `{"id":"chatcmpl-1","choices":[]}` {
		t.Fatalf("stored response = %+v", it.Response)
	}
}

func TestEngineHandleRedactsAuthHeaderInCassette(t *testing.T) {
	up := &fakeUpstream{resp: jsonUpstreamResponse(200, `{}`)}
	store := &fakeStore{}
	e := &Engine{Upstream: up, Store: store}

	if _, err := e.Handle(context.Background(), newRequest(t, `{}`)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if _, ok := store.appended[0].Request.Headers["Authorization"]; ok {
		t.Fatal("cassette stored the Authorization header; it must be redacted")
	}
	// Forwarding must still have received it — checked in the previous test —
	// this test only asserts what gets persisted to disk.
}

func TestEngineHandleSanitizesOnlyTheHashNotTheStoredBodyOrUpstreamCall(t *testing.T) {
	up := &fakeUpstream{resp: jsonUpstreamResponse(200, `{}`)}
	store := &fakeStore{}
	e := &Engine{
		Upstream:  up,
		Store:     store,
		Sanitizer: sanitize.New(sanitize.UUID),
	}

	bodyA := `{"request_id":"11111111-1111-1111-1111-111111111111","model":"gpt-4o-mini"}`
	bodyB := `{"request_id":"22222222-2222-2222-2222-222222222222","model":"gpt-4o-mini"}`

	if _, err := e.Handle(context.Background(), newRequest(t, bodyA)); err != nil {
		t.Fatalf("Handle (A): %v", err)
	}
	if string(up.lastBody) != bodyA {
		t.Fatalf("upstream received sanitized body %q instead of the original %q", up.lastBody, bodyA)
	}
	if store.appended[0].Request.Body != bodyA {
		t.Fatalf("cassette stored sanitized body %q instead of the original %q", store.appended[0].Request.Body, bodyA)
	}

	if _, err := e.Handle(context.Background(), newRequest(t, bodyB)); err != nil {
		t.Fatalf("Handle (B): %v", err)
	}

	if store.appended[0].RequestHash != store.appended[1].RequestHash {
		t.Fatalf("requests differing only by a sanitized UUID hashed differently: %q vs %q",
			store.appended[0].RequestHash, store.appended[1].RequestHash)
	}
}

func TestEngineHandleUpstreamErrorRecordsNothing(t *testing.T) {
	up := &fakeUpstream{err: context.DeadlineExceeded}
	store := &fakeStore{}
	e := &Engine{Upstream: up, Store: store}

	if _, err := e.Handle(context.Background(), newRequest(t, `{}`)); err == nil {
		t.Fatal("Handle: want error when upstream fails, got nil")
	}
	if len(store.appended) != 0 {
		t.Fatalf("cassette has %d interactions after an upstream failure, want 0", len(store.appended))
	}
}

func TestEngineHandleStoreAppendErrorFailsTheRequest(t *testing.T) {
	up := &fakeUpstream{resp: jsonUpstreamResponse(200, `{}`)}
	store := &fakeStore{err: io.ErrClosedPipe}
	e := &Engine{Upstream: up, Store: store}

	if _, err := e.Handle(context.Background(), newRequest(t, `{}`)); err == nil {
		t.Fatal("Handle: want error when the cassette store fails to append, got nil")
	}
}

func TestEngineHandleRejectsNonJSONBodyBeforeCallingUpstream(t *testing.T) {
	up := &fakeUpstream{resp: jsonUpstreamResponse(200, `{}`)}
	store := &fakeStore{}
	e := &Engine{Upstream: up, Store: store}

	if _, err := e.Handle(context.Background(), newRequest(t, `not json`)); err == nil {
		t.Fatal("Handle: want error for a non-JSON body, got nil")
	}
	if up.called {
		t.Fatal("upstream was called for a request that should have failed canonicalization first")
	}
	if len(store.appended) != 0 {
		t.Fatal("a rejected request must not be recorded")
	}
}

func TestEngineHandleEmptyBodyOK(t *testing.T) {
	up := &fakeUpstream{resp: jsonUpstreamResponse(200, `{}`)}
	store := &fakeStore{}
	e := &Engine{Upstream: up, Store: store}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if _, err := e.Handle(context.Background(), req); err != nil {
		t.Fatalf("Handle: unexpected error for an empty body: %v", err)
	}
	if !hashPattern.MatchString(store.appended[0].RequestHash) {
		t.Fatalf("RequestHash %q does not look like sha256:<hex>", store.appended[0].RequestHash)
	}
}

func TestEngineHandleStreamsSSEAndRecordsChunks(t *testing.T) {
	frames := []string{
		"data: {\"delta\":\"Hel\"}\n\n",
		"data: {\"delta\":\"lo\"}\n\n",
		"data: [DONE]\n\n",
	}
	readers := make([]io.Reader, len(frames))
	for i, f := range frames {
		readers[i] = strings.NewReader(f)
	}

	up := &fakeUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(io.MultiReader(readers...)),
	}}
	store := &fakeStore{}
	e := &Engine{Upstream: up, Store: store}

	resp, err := e.Handle(context.Background(), newRequest(t, `{"model":"gpt-4o-mini","stream":true}`))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	defer resp.Body.Close()

	if resp.ContentLength != -1 {
		t.Fatalf("ContentLength = %d, want -1 (unknown/streamed)", resp.ContentLength)
	}

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := strings.Join(frames, "")
	if string(got) != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	// Append happens before the pipe reports EOF (see handleStream), so it
	// is guaranteed to have already run by the time ReadAll returns above.
	if len(store.appended) != 1 {
		t.Fatalf("cassette has %d interactions, want 1", len(store.appended))
	}
	it := store.appended[0]
	if !it.Response.Stream {
		t.Fatal("stored interaction: Response.Stream = false, want true")
	}
	if len(it.Response.Chunks) != len(frames) {
		t.Fatalf("stored %d chunks, want %d", len(it.Response.Chunks), len(frames))
	}
	for i, f := range frames {
		if it.Response.Chunks[i].Data != f {
			t.Fatalf("chunk %d = %q, want %q", i, it.Response.Chunks[i].Data, f)
		}
	}
}

// errAfterReader returns data once, then a fixed error — simulating an
// upstream connection that fails partway through a stream.
type errAfterReader struct {
	data []byte
	err  error
	sent bool
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, r.err
	}
	r.sent = true
	return copy(p, r.data), nil
}

func TestEngineHandleStreamUpstreamErrorRecordsNothing(t *testing.T) {
	up := &fakeUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(&errAfterReader{data: []byte("data: partial\n\n"), err: io.ErrUnexpectedEOF}),
	}}
	store := &fakeStore{}
	e := &Engine{Upstream: up, Store: store}

	resp, err := e.Handle(context.Background(), newRequest(t, `{"model":"gpt-4o-mini","stream":true}`))
	if err != nil {
		t.Fatalf("Handle: %v (the failure happens asynchronously, Handle itself should still succeed)", err)
	}
	defer resp.Body.Close()

	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("ReadAll: want an error reading a stream that failed upstream, got nil")
	}

	// CloseWithError happens after deciding not to append, so observing the
	// error on read guarantees that decision has already been made.
	if len(store.appended) != 0 {
		t.Fatalf("cassette has %d interactions after an upstream failure mid-stream, want 0", len(store.appended))
	}
}
