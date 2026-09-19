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
	"net/http"
	"strings"

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
// Replay is not implemented by this Engine — see mvp.md's J9 milestone.
type Engine struct {
	Upstream Upstream
	Store    CassetteStore

	// Sanitizer runs on a copy of the request body before it is hashed. A
	// nil Sanitizer means no sanitization: volatile values like request IDs
	// or timestamps will make otherwise-identical requests hash
	// differently.
	Sanitizer *sanitize.Sanitizer
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

	canonicalBody, err := e.canonicalizeBody(reqBody)
	if err != nil {
		return nil, fmt.Errorf("engine: canonicalize request body: %w", err)
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
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("engine: read upstream response body: %w", err)
	}

	fp := fingerprint.Compute(fingerprint.Input{
		Method:        req.Method,
		Path:          req.URL.Path,
		Headers:       filterHeaders(req.Header, defaultFingerprintHeaders),
		CanonicalBody: canonicalBody,
	})

	it := cassette.Interaction{
		Version: cassette.CurrentVersion,
		ID:      newID(),
		Request: cassette.RequestSnapshot{
			Method:  req.Method,
			URL:     req.URL.String(),
			Headers: redactedHeaders(req.Header, defaultRedactedHeaders),
			Body:    string(reqBody),
		},
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

// canonicalizeBody runs the sanitize -> JCS pipeline from internal/sanitize
// and internal/fingerprint. An empty body (e.g. a bodyless GET) skips both
// steps rather than erroring on invalid JSON.
func (e *Engine) canonicalizeBody(body []byte) ([]byte, error) {
	if len(body) == 0 {
		return nil, nil
	}

	sanitized := body
	if e.Sanitizer != nil {
		var err error
		sanitized, err = e.Sanitizer.Sanitize(body)
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
