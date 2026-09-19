// Package proxy is a thin net/http adaptation layer in front of the engine.
// It never decides whether to record or replay, how to hash a request, or
// how a cassette is stored — it only translates http.Request/ResponseWriter
// to and from Handler calls. Streaming responses (SSE) are forwarded as raw
// bytes, chunk by chunk, never re-parsed or re-serialized (see mvp.md §3;
// full streaming support lands with the J10 milestone).
package proxy

import (
	"context"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// Handler is the shape engine.Engine.Handle satisfies. Proxy depends on
// this interface rather than importing the engine package, so it never has
// an opinion on how a request is recorded, replayed, or hashed.
type Handler interface {
	Handle(ctx context.Context, req *http.Request) (*http.Response, error)
}

// Proxy adapts a Handler to net/http.
type Proxy struct {
	Handler Handler

	// ErrorLog receives request-handling errors. It defaults to log.Printf
	// if nil.
	ErrorLog func(format string, args ...any)
}

// hopByHopResponseHeaders are recomputed from the buffered response rather
// than copied from upstream: the upstream's framing (chunked, a specific
// Content-Length) does not necessarily match how Proxy re-serves an
// already-fully-read body.
var hopByHopResponseHeaders = []string{"Content-Length", "Transfer-Encoding"}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp, err := p.Handler.Handle(r.Context(), r)
	if err != nil {
		p.logf("tapelock: %v", err)
		http.Error(w, "tapelock: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	dst := w.Header()
	for name, values := range resp.Header {
		if isHopByHopResponseHeader(name) {
			continue
		}
		for _, v := range values {
			dst.Add(name, v)
		}
	}
	if resp.ContentLength >= 0 {
		dst.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}

	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		p.logf("tapelock: write response to client: %v", err)
	}
}

func isHopByHopResponseHeader(name string) bool {
	for _, h := range hopByHopResponseHeaders {
		if strings.EqualFold(h, name) {
			return true
		}
	}
	return false
}

func (p *Proxy) logf(format string, args ...any) {
	if p.ErrorLog != nil {
		p.ErrorLog(format, args...)
		return
	}
	log.Printf(format, args...)
}
