package proxy_test

// TestRecordEndToEnd wires proxy.Proxy, engine.Engine, proxy.Upstream, and a
// real cassette.Manager together exactly as `tapelock record` will: a
// client talks to the Tapelock proxy, which forwards to a fake "OpenAI"
// server and durably records the interaction to a file on disk. This is
// the J7-J8 milestone from mvp.md §16: "client -> tapelock -> upstream,
// puis capture."

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tapelock/tapelock/internal/cassette"
	"github.com/tapelock/tapelock/internal/engine"
	"github.com/tapelock/tapelock/internal/proxy"
)

func TestRecordEndToEnd(t *testing.T) {
	fakeOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"finish_reason":"stop"}]}`))
	}))
	defer fakeOpenAI.Close()

	base, err := url.Parse(fakeOpenAI.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	cassettePath := filepath.Join(t.TempDir(), "openai-chat.jsonl")
	store, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	eng := &engine.Engine{
		Upstream: proxy.NewUpstream(base, nil),
		Store:    store,
	}
	tapelock := httptest.NewServer(&proxy.Proxy{Handler: eng})
	defer tapelock.Close()

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(tapelock.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Re-read the cassette from a fresh Manager: the real deliverable is a
	// file on disk, not just in-memory state on the instance that wrote it.
	onDisk, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("re-reading cassette: %v", err)
	}
	interactions := onDisk.Read()
	if len(interactions) != 1 {
		t.Fatalf("cassette has %d interactions, want 1", len(interactions))
	}
	if interactions[0].Request.Body != body {
		t.Fatalf("stored request body = %q, want %q", interactions[0].Request.Body, body)
	}
	if interactions[0].Response.Status != http.StatusOK {
		t.Fatalf("stored response status = %d, want 200", interactions[0].Response.Status)
	}
}

// TestRecordThenReplayEndToEnd is the J9 milestone from mvp.md §16: record
// a real interaction, then serve it back from the cassette alone — with no
// upstream reachable at all — and confirm an unrecorded request is a
// deterministic miss rather than a silent fallback to a live call.
func TestRecordThenReplayEndToEnd(t *testing.T) {
	fakeOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"finish_reason":"stop"}]}`))
	}))
	defer fakeOpenAI.Close()

	base, err := url.Parse(fakeOpenAI.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	cassettePath := filepath.Join(t.TempDir(), "openai-chat.jsonl")
	recordStore, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	recorder := httptest.NewServer(&proxy.Proxy{Handler: &engine.Engine{
		Upstream: proxy.NewUpstream(base, nil),
		Store:    recordStore,
	}})
	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	recordResp, err := http.Post(recorder.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("record POST: %v", err)
	}
	recordResp.Body.Close()
	recorder.Close()
	fakeOpenAI.Close() // the replay side must never need this again

	replayStore, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("NewManager (replay): %v", err)
	}
	replayer := httptest.NewServer(&proxy.Proxy{Handler: &engine.ReplayEngine{Store: replayStore}})
	defer replayer.Close()

	t.Run("hit", func(t *testing.T) {
		resp, err := http.Post(replayer.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("replay POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if string(got) != `{"id":"chatcmpl-1","choices":[{"finish_reason":"stop"}]}` {
			t.Fatalf("body = %q", got)
		}
	})

	t.Run("miss", func(t *testing.T) {
		unrecorded := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"never recorded"}]}`
		resp, err := http.Post(replayer.URL+"/v1/chat/completions", "application/json", strings.NewReader(unrecorded))
		if err != nil {
			t.Fatalf("replay POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
		}
		if resp.Header.Get(proxy.MissHeader) != "1" {
			t.Fatalf("%s header = %q, want %q", proxy.MissHeader, resp.Header.Get(proxy.MissHeader), "1")
		}
	})
}

// TestStreamRecordThenReplayEndToEnd is the J10 milestone from mvp.md §16:
// an SSE response, flushed as several real network writes by a genuine
// httptest server, is recorded and then replayed byte-for-byte with no
// upstream involved at all.
func TestStreamRecordThenReplayEndToEnd(t *testing.T) {
	frames := []string{
		"data: {\"delta\":\"Hel\"}\n\n",
		"data: {\"delta\":\"lo\"}\n\n",
		"data: [DONE]\n\n",
	}

	fakeOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for i, f := range frames {
			if i > 0 {
				// Without a gap, even flushed writes can arrive close
				// enough together that a single client Read() coalesces
				// them — a real property of TCP, not a bug (mvp.md's
				// proxy is byte-oriented and makes no promise that one
				// flush becomes one recorded chunk). The delay is only
				// here to make this test's "count the chunks" assertion
				// non-flaky; it is not something Tapelock relies on.
				time.Sleep(10 * time.Millisecond)
			}
			fmt.Fprint(w, f)
			flusher.Flush()
		}
	}))
	defer fakeOpenAI.Close()

	base, err := url.Parse(fakeOpenAI.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	cassettePath := filepath.Join(t.TempDir(), "openai-chat.jsonl")
	recordStore, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	recorder := httptest.NewServer(&proxy.Proxy{Handler: &engine.Engine{
		Upstream: proxy.NewUpstream(base, nil),
		Store:    recordStore,
	}})

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}],"stream":true}`
	recordResp, err := http.Post(recorder.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("record POST: %v", err)
	}
	got, err := io.ReadAll(recordResp.Body)
	if err != nil {
		t.Fatalf("record ReadAll: %v", err)
	}
	recordResp.Body.Close()
	recorder.Close()
	fakeOpenAI.Close()

	want := strings.Join(frames, "")
	if string(got) != want {
		t.Fatalf("client received %q while recording, want %q", got, want)
	}

	onDisk, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("re-reading cassette: %v", err)
	}
	interactions := onDisk.Read()
	if len(interactions) != 1 || !interactions[0].Response.Stream {
		t.Fatalf("cassette = %+v, want exactly one streamed interaction", interactions)
	}
	if len(interactions[0].Response.Chunks) < 2 {
		t.Fatalf("recorded %d chunks, want at least 2 (one per flush)", len(interactions[0].Response.Chunks))
	}

	replayStore, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("NewManager (replay): %v", err)
	}
	replayer := httptest.NewServer(&proxy.Proxy{Handler: &engine.ReplayEngine{Store: replayStore}})
	defer replayer.Close()

	replayResp, err := http.Post(replayer.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("replay POST: %v", err)
	}
	defer replayResp.Body.Close()

	replayed, err := io.ReadAll(replayResp.Body)
	if err != nil {
		t.Fatalf("replay ReadAll: %v", err)
	}
	if string(replayed) != want {
		t.Fatalf("replayed body = %q, want %q", replayed, want)
	}
}

// TestStreamClientDisconnectRecordsNothing is the other half of mvp.md §10:
// if the client goes away mid-stream, the interaction must never be
// recorded — a partial recording would be a corrupt, unreplayable fixture.
func TestStreamClientDisconnectRecordsNothing(t *testing.T) {
	fakeOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for i := 0; i < 20; i++ {
			fmt.Fprintf(w, "data: {\"i\":%d}\n\n", i)
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}))
	defer fakeOpenAI.Close()

	base, err := url.Parse(fakeOpenAI.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	cassettePath := filepath.Join(t.TempDir(), "openai-chat.jsonl")
	store, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	tapelock := httptest.NewServer(&proxy.Proxy{Handler: &engine.Engine{
		Upstream: proxy.NewUpstream(base, nil),
		Store:    store,
	}})
	defer tapelock.Close()

	ctx, cancel := context.WithCancel(context.Background())
	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tapelock.URL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("read first chunk: %v", err)
	}
	cancel() // simulate the client giving up mid-stream
	resp.Body.Close()

	// Give the engine's goroutine time to notice the canceled context and
	// decide not to record.
	time.Sleep(300 * time.Millisecond)

	onDisk, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("re-reading cassette: %v", err)
	}
	if got := len(onDisk.Read()); got != 0 {
		t.Fatalf("cassette has %d interactions after a client disconnect mid-stream, want 0", got)
	}
}
