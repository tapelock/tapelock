package engine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tapelock/tapelock/internal/cassette"
	"github.com/tapelock/tapelock/internal/sanitize"
)

type fakeLookup struct {
	byHash map[string]cassette.Interaction
}

func (f *fakeLookup) Lookup(hash string) (cassette.Interaction, bool) {
	it, ok := f.byHash[hash]
	return it, ok
}

func newReplayRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestReplayEngineHandleServesRecordedResponse(t *testing.T) {
	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`

	fp, err := fingerprintRequest(nil, newReplayRequest(t, body), []byte(body))
	if err != nil {
		t.Fatalf("fingerprintRequest: %v", err)
	}

	recorded := cassette.Interaction{
		Version:     cassette.CurrentVersion,
		RequestHash: fp.Hash,
		Response: cassette.ResponseSnapshot{
			Status:  200,
			Headers: cassette.Headers{"Content-Type": {"application/json"}},
			Body:    `{"id":"chatcmpl-1"}`,
		},
	}

	re := &ReplayEngine{Store: &fakeLookup{byHash: map[string]cassette.Interaction{fp.Hash: recorded}}}

	resp, err := re.Handle(context.Background(), newReplayRequest(t, body))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != `{"id":"chatcmpl-1"}` {
		t.Fatalf("body = %q", got)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
}

func TestReplayEngineHandleMissReturnsMissError(t *testing.T) {
	re := &ReplayEngine{Store: &fakeLookup{byHash: map[string]cassette.Interaction{}}}

	_, err := re.Handle(context.Background(), newReplayRequest(t, `{"model":"gpt-4o-mini"}`))
	if err == nil {
		t.Fatal("Handle: want error on a cassette miss, got nil")
	}

	var miss *MissError
	if !errors.As(err, &miss) {
		t.Fatalf("Handle: want a *MissError, got %T: %v", err, err)
	}
	if !miss.CassetteMiss() {
		t.Fatal("MissError.CassetteMiss() = false, want true")
	}
	if miss.Method != http.MethodPost || miss.Path != "/v1/chat/completions" {
		t.Fatalf("MissError = %+v, unexpected method/path", miss)
	}
}

func TestReplayEngineHandleRejectsStreamedRecording(t *testing.T) {
	body := `{"model":"gpt-4o-mini"}`
	fp, err := fingerprintRequest(nil, newReplayRequest(t, body), []byte(body))
	if err != nil {
		t.Fatalf("fingerprintRequest: %v", err)
	}

	recorded := cassette.Interaction{
		Version:     cassette.CurrentVersion,
		RequestHash: fp.Hash,
		Response: cassette.ResponseSnapshot{
			Status: 200,
			Stream: true,
			Chunks: []cassette.ResponseChunk{{Data: "data: hi\n\n", DelayMS: 0}},
		},
	}
	re := &ReplayEngine{Store: &fakeLookup{byHash: map[string]cassette.Interaction{fp.Hash: recorded}}}

	_, err = re.Handle(context.Background(), newReplayRequest(t, body))
	if err == nil {
		t.Fatal("Handle: want error for a streamed recording, got nil")
	}
	var miss *MissError
	if errors.As(err, &miss) {
		t.Fatal("a streamed recording should not be reported as a cassette miss")
	}
}

// TestRecordAndReplayAgree is the property that matters most: whatever
// Engine (record) hashes a request under, ReplayEngine (replay) must
// compute the exact same hash for an equivalent request, even when the
// two requests differ in ways sanitization is supposed to erase.
func TestRecordAndReplayAgree(t *testing.T) {
	sanitizer := sanitize.New(sanitize.UUID)

	recordBody := `{"request_id":"11111111-1111-1111-1111-111111111111","model":"gpt-4o-mini","messages":[]}`
	up := &fakeUpstream{resp: jsonUpstreamResponse(200, `{"id":"chatcmpl-1"}`)}
	store := &fakeStore{}
	rec := &Engine{Upstream: up, Store: store, Sanitizer: sanitizer}

	if _, err := rec.Handle(context.Background(), newRequest(t, recordBody)); err != nil {
		t.Fatalf("record Handle: %v", err)
	}
	recordedHash := store.appended[0].RequestHash

	replayBody := `{"model":"gpt-4o-mini","request_id":"22222222-2222-2222-2222-222222222222","messages":[]}`
	lookup := &fakeLookup{byHash: map[string]cassette.Interaction{recordedHash: store.appended[0]}}
	rep := &ReplayEngine{Store: lookup, Sanitizer: sanitizer}

	resp, err := rep.Handle(context.Background(), newReplayRequest(t, replayBody))
	if err != nil {
		t.Fatalf("replay Handle: %v (record and replay likely hashed differently)", err)
	}
	defer resp.Body.Close()

	got, _ := io.ReadAll(resp.Body)
	if string(got) != `{"id":"chatcmpl-1"}` {
		t.Fatalf("replayed body = %q", got)
	}
}
