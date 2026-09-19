package proxy_test

// TestRecordEndToEnd wires proxy.Proxy, engine.Engine, proxy.Upstream, and a
// real cassette.Manager together exactly as `tapelock record` will: a
// client talks to the Tapelock proxy, which forwards to a fake "OpenAI"
// server and durably records the interaction to a file on disk. This is
// the J7-J8 milestone from mvp.md §16: "client -> tapelock -> upstream,
// puis capture."

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

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
