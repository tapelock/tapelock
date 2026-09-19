package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tapelock/tapelock/internal/cassette"
)

func TestExitCodeFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, exitOK},
		{"plain error (cobra usage/flag validation)", errors.New("boom"), exitConfigError},
		{"configErrorf", configErrorf("bad %s", "flag"), exitConfigError},
		{"assertion failure", &cliError{code: exitAssertionFail, err: errors.New("x")}, exitAssertionFail},
		{"cassette miss", &cliError{code: exitCassetteMiss, err: errors.New("x")}, exitCassetteMiss},
		{"internal error", &cliError{code: exitInternalError, err: errors.New("x")}, exitInternalError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCodeFor(tt.err); got != tt.want {
				t.Fatalf("exitCodeFor(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// syncBuffer is an io.Writer safe for one goroutine writing (a running CLI
// command) while the test goroutine polls its contents.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var listenAddrPattern = regexp.MustCompile(`listening on (http://\S+)`)

func waitForAddr(t *testing.T, out *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m := listenAddrPattern.FindStringSubmatch(out.String()); m != nil {
			return m[1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a listen address in output:\n%s", out.String())
	return ""
}

// runServerCmd starts a long-running command (record/replay) in the
// background and returns its captured output plus a channel that receives
// root.ExecuteContext's result once ctx is canceled.
func runServerCmd(ctx context.Context, args ...string) (*syncBuffer, <-chan error) {
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		root := newRootCmd()
		root.SetOut(out)
		root.SetErr(out)
		root.SilenceErrors = true
		root.SilenceUsage = true
		root.SetArgs(args)
		done <- root.ExecuteContext(ctx)
	}()
	return out, done
}

// runCLI runs a one-shot command (check, or a record/replay expected to
// fail before starting a server) to completion and returns its output.
func runCLI(args ...string) (string, error) {
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestRecordCommandRequiresUpstream(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")

	_, err := runCLI("record", "--cassette", cassettePath)
	if got := exitCodeFor(err); got != exitConfigError {
		t.Fatalf("exit code = %d, want %d (exitConfigError); err = %v", got, exitConfigError, err)
	}
}

func TestRecordCommandRejectsInvalidUpstream(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")

	_, err := runCLI("record", "--cassette", cassettePath, "--upstream", "not-a-url")
	if got := exitCodeFor(err); got != exitConfigError {
		t.Fatalf("exit code = %d, want %d (exitConfigError); err = %v", got, exitConfigError, err)
	}
}

func TestCheckCommandOnCleanCassette(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")
	store, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := store.Append(cassette.Interaction{
		Version: cassette.CurrentVersion,
		ID:      "ok-1",
		Response: cassette.ResponseSnapshot{
			Status: 200,
			Body:   `{"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
		},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	out, err := runCLI("check", "--cassette", cassettePath, "--status", "200", "--max-total-tokens", "100")
	if got := exitCodeFor(err); got != exitOK {
		t.Fatalf("exit code = %d, want %d (exitOK); err = %v\noutput:\n%s", got, exitOK, err, out)
	}
}

func TestCheckCommandReportsAssertionFailure(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")
	store, err := cassette.NewManager(cassettePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := store.Append(cassette.Interaction{
		Version:  cassette.CurrentVersion,
		ID:       "bad-1",
		Response: cassette.ResponseSnapshot{Status: 500, Body: `{}`},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	out, err := runCLI("check", "--cassette", cassettePath, "--status", "200")
	if got := exitCodeFor(err); got != exitAssertionFail {
		t.Fatalf("exit code = %d, want %d (exitAssertionFail); err = %v\noutput:\n%s", got, exitAssertionFail, err, out)
	}
	if !strings.Contains(out, "FAIL bad-1") {
		t.Fatalf("output missing failure detail for bad-1:\n%s", out)
	}
}

// TestRecordThenReplayThenCheckEndToEnd exercises the full CLI wiring for
// the J12 milestone: record a real interaction through the compiled
// command graph, validate it with check, then replay it with no upstream
// involved.
func TestRecordThenReplayThenCheckEndToEnd(t *testing.T) {
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer fakeUpstream.Close()

	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")
	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`

	recordCtx, cancelRecord := context.WithCancel(context.Background())
	recordOut, recordDone := runServerCmd(recordCtx,
		"record", "--cassette", cassettePath, "--upstream", fakeUpstream.URL, "--listen", "127.0.0.1:0")

	recordAddr := waitForAddr(t, recordOut)
	resp, err := http.Post(recordAddr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("record POST: %v", err)
	}
	resp.Body.Close()

	cancelRecord()
	if err := <-recordDone; err != nil {
		t.Fatalf("record command exited with error: %v", err)
	}

	checkOut, err := runCLI("check", "--cassette", cassettePath, "--status", "200", "--max-total-tokens", "100")
	if got := exitCodeFor(err); got != exitOK {
		t.Fatalf("check exit code = %d, want %d; err = %v\noutput:\n%s", got, exitOK, err, checkOut)
	}

	replayCtx, cancelReplay := context.WithCancel(context.Background())
	replayOut, replayDone := runServerCmd(replayCtx,
		"replay", "--cassette", cassettePath, "--listen", "127.0.0.1:0")

	replayAddr := waitForAddr(t, replayOut)
	replayResp, err := http.Post(replayAddr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("replay POST: %v", err)
	}
	got, err := io.ReadAll(replayResp.Body)
	replayResp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(string(got), "chatcmpl-1") {
		t.Fatalf("replayed body = %q, want it to contain the recorded response", got)
	}

	cancelReplay()
	if err := <-replayDone; err != nil {
		t.Fatalf("replay command (hit only) exited with error: %v", err)
	}
}

func TestReplayCommandExitsWithMissCodeOnUnrecordedRequest(t *testing.T) {
	// An empty cassette: NewManager treats a missing file as empty, so no
	// setup is needed beyond picking a path nothing has written to yet.
	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")

	ctx, cancel := context.WithCancel(context.Background())
	out, done := runServerCmd(ctx, "replay", "--cassette", cassettePath, "--listen", "127.0.0.1:0")

	addr := waitForAddr(t, out)
	resp, err := http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-4o-mini"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	cancel()
	err = <-done
	if got := exitCodeFor(err); got != exitCassetteMiss {
		t.Fatalf("exit code = %d, want %d (exitCassetteMiss); err = %v", got, exitCassetteMiss, err)
	}
}
