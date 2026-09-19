// Package engine orchestrates record/replay behavior. It knows nothing about
// http.Server, files on disk, or the CLI — it depends only on the ports
// (Upstream, CassetteStore) implemented by the other internal packages.
// This is what lets 90% of Tapelock's behavior be unit-tested without ever
// starting the proxy.
package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/tapelock/tapelock/internal/cassette"
	"github.com/tapelock/tapelock/internal/fingerprint"
	"github.com/tapelock/tapelock/internal/sanitize"
)

// Upstream forwards a request to the real LLM API and returns its response.
// internal/proxy implements this with a real *http.Client; tests use a
// fake.
type Upstream interface {
	Do(req *http.Request) (*http.Response, error)
}

// CassetteStore is the subset of cassette.Manager the record engine needs.
type CassetteStore interface {
	Append(cassette.Interaction) error
}

// defaultFingerprintHeaders is the header allowlist used to compute a
// request's Fingerprint until match.headers becomes configurable via
// tapelock.yaml (Layer 2). Everything else — notably Authorization, Date,
// User-Agent, Content-Length — is excluded on purpose: hashing them would
// make replay fragile across harmless client differences (see mvp.md §8).
var defaultFingerprintHeaders = []string{"content-type"}

// defaultRedactedHeaders are stripped from the request headers a cassette
// stores. They are still forwarded to the upstream — recording must still
// authenticate — but a cassette committed to git must never leak them (see
// the PRD's OWASP §2.3).
var defaultRedactedHeaders = []string{"authorization", "x-api-key", "openai-organization"}

// Engine records a request/response pair: it forwards the request to
// Upstream unmodified, then appends the interaction to Store before
// returning the response to the caller. A Store failure fails the whole
// request (502) rather than silently serving a response that was never
// durably recorded.
//
// See ReplayEngine (replay.go) for the read-only counterpart.
type Engine struct {
	Upstream Upstream
	Store    CassetteStore

	// Sanitizer runs on a copy of the request body before it is hashed. A
	// nil Sanitizer means no sanitization: volatile values like request IDs
	// or timestamps will make otherwise-identical requests hash
	// differently.
	Sanitizer *sanitize.Sanitizer

	// Logf receives errors that happen after Handle has already returned a
	// response to the caller — which, for a streamed response, can include
	// a cassette append failure, since by then the client has already
	// received the full body. It defaults to log.Printf if nil.
	Logf func(format string, args ...any)
}

// Handle reads req fully, forwards it to Upstream unchanged, records the
// interaction, and returns the upstream response. The body is assumed to
// be JSON, matching S1's OpenAI Chat Completions scope; a non-JSON,
// non-empty body fails canonicalization before Upstream is ever called, so
// a malformed request never costs a paid API call.
func (e *Engine) Handle(ctx context.Context, req *http.Request) (*http.Response, error) {
	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("engine: read request body: %w", err)
	}
	req.Body.Close()

	fp, err := fingerprintRequest(e.Sanitizer, req, reqBody)
	if err != nil {
		return nil, fmt.Errorf("engine: fingerprint request: %w", err)
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("engine: build upstream request: %w", err)
	}
	upstreamReq.Header = req.Header.Clone()

	resp, err := e.Upstream.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("engine: upstream request failed: %w", err)
	}

	if isEventStream(resp.Header.Get("Content-Type")) {
		// handleStream takes ownership of resp.Body and returns before the
		// stream finishes; Handle must not close it here.
		return e.handleStream(req, reqBody, fp, resp), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("engine: read upstream response body: %w", err)
	}

	it := cassette.Interaction{
		Version:     cassette.CurrentVersion,
		ID:          newID(),
		Request:     e.requestSnapshot(req, reqBody),
		RequestHash: fp.Hash,
		Response: cassette.ResponseSnapshot{
			Status:  resp.StatusCode,
			Headers: cassette.Headers(resp.Header),
			Body:    string(respBody),
		},
	}
	if err := e.Store.Append(it); err != nil {
		return nil, fmt.Errorf("engine: append to cassette: %w", err)
	}

	return &http.Response{
		StatusCode:    resp.StatusCode,
		Header:        resp.Header,
		Body:          io.NopCloser(bytes.NewReader(respBody)),
		ContentLength: int64(len(respBody)),
	}, nil
}

// isEventStream reports whether contentType is Server-Sent Events,
// ignoring any charset or other parameters.
func isEventStream(contentType string) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	return strings.EqualFold(mediaType, "text/event-stream")
}

// streamReadSize bounds each read from an upstream SSE response. It is
// deliberately not aligned to SSE frame ("\n\n") boundaries: the proxy
// stays byte-oriented, with no SSE-specific parsing (see mvp.md §3).
const streamReadSize = 32 * 1024

// handleStream forwards an SSE response to the client as bytes arrive, via
// an io.Pipe, while recording each read as one cassette.ResponseChunk with
// its arrival time relative to the previous read. It returns immediately
// with a response whose Body streams from the pipe — the caller must not
// close resp.Body; the background goroutine does, once the stream ends.
//
// The cassette entry is appended only once the stream ends cleanly
// (upstream EOF). A stream that ends any other way — the client
// disconnects (which cancels req's context, and so the shared upstream
// request context), or upstream itself fails — is never recorded: a
// partial recording would be a corrupt, unreplayable fixture (mvp.md §10).
func (e *Engine) handleStream(req *http.Request, reqBody []byte, fp fingerprint.Fingerprint, resp *http.Response) *http.Response {
	pr, pw := io.Pipe()

	go func() {
		defer resp.Body.Close()

		var chunks []cassette.ResponseChunk
		buf := make([]byte, streamReadSize)
		last := time.Now()

		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				now := time.Now()
				chunks = append(chunks, cassette.ResponseChunk{
					Data:    string(buf[:n]),
					DelayMS: now.Sub(last).Milliseconds(),
				})
				last = now

				if _, writeErr := pw.Write(buf[:n]); writeErr != nil {
					// The client side is gone: stop reading from upstream
					// (no point paying for tokens nobody will see) and
					// record nothing.
					pw.CloseWithError(writeErr)
					return
				}
			}

			if readErr != nil {
				if readErr != io.EOF {
					// Upstream failed, or req's context was canceled by a
					// client disconnect: the stream is incomplete.
					pw.CloseWithError(readErr)
					return
				}

				it := cassette.Interaction{
					Version:     cassette.CurrentVersion,
					ID:          newID(),
					Request:     e.requestSnapshot(req, reqBody),
					RequestHash: fp.Hash,
					Response: cassette.ResponseSnapshot{
						Status:  resp.StatusCode,
						Headers: cassette.Headers(resp.Header),
						Stream:  true,
						Chunks:  chunks,
					},
				}
				// Append before closing the pipe: by the time the client
				// sees end-of-stream, the recording is already durable.
				// Handle has already returned this stream's response to
				// the caller, so an append failure can no longer fail the
				// request the way the buffered path does — it can only be
				// logged.
				if err := e.Store.Append(it); err != nil {
					e.logf("engine: append streamed cassette entry: %v", err)
				}
				pw.Close()
				return
			}
		}
	}()

	return &http.Response{
		StatusCode:    resp.StatusCode,
		Header:        resp.Header,
		Body:          pr,
		ContentLength: -1,
	}
}

func (e *Engine) requestSnapshot(req *http.Request, reqBody []byte) cassette.RequestSnapshot {
	return cassette.RequestSnapshot{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: redactedHeaders(req.Header, defaultRedactedHeaders),
		Body:    string(reqBody),
	}
}

func (e *Engine) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// fingerprintRequest computes the Fingerprint that both Engine (record) and
// ReplayEngine use to key a cassette entry. The two MUST hash identically —
// if they ever diverged, a replay could never hit what was just recorded.
func fingerprintRequest(s *sanitize.Sanitizer, req *http.Request, body []byte) (fingerprint.Fingerprint, error) {
	canonicalBody, err := canonicalizeBody(s, body)
	if err != nil {
		return fingerprint.Fingerprint{}, err
	}

	return fingerprint.Compute(fingerprint.Input{
		Method:        req.Method,
		Path:          req.URL.Path,
		Headers:       filterHeaders(req.Header, defaultFingerprintHeaders),
		CanonicalBody: canonicalBody,
	}), nil
}

// canonicalizeBody runs the sanitize -> JCS pipeline from internal/sanitize
// and internal/fingerprint. An empty body (e.g. a bodyless GET) skips both
// steps rather than erroring on invalid JSON.
func canonicalizeBody(s *sanitize.Sanitizer, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return nil, nil
	}

	sanitized := body
	if s != nil {
		var err error
		sanitized, err = s.Sanitize(body)
		if err != nil {
			return nil, fmt.Errorf("sanitize: %w", err)
		}
	}

	canonical, err := fingerprint.Canonicalize(sanitized)
	if err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	return canonical, nil
}

func filterHeaders(h http.Header, allow []string) map[string][]string {
	out := make(map[string][]string, len(allow))
	for _, name := range allow {
		if v := h.Values(name); len(v) > 0 {
			out[name] = v
		}
	}
	return out
}

func redactedHeaders(h http.Header, blocklist []string) cassette.Headers {
	out := make(cassette.Headers, len(h))
	for name, values := range h {
		if containsFold(blocklist, name) {
			continue
		}
		out[name] = values
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read only fails if the OS RNG is unavailable, which
		// would already be fatal for the process; panicking surfaces that
		// immediately instead of silently recording a zero ID.
		panic(fmt.Sprintf("engine: crypto/rand unavailable: %v", err))
	}
	return hex.EncodeToString(b)
}
