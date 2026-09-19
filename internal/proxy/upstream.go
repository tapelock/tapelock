package proxy

import (
	"net/http"
	"net/url"
)

// Upstream implements engine.Upstream (structurally — this package does not
// import engine) by rewriting a request's scheme and host to a fixed
// upstream base URL, then sending it with a real *http.Client.
//
// It is the only place in Tapelock that makes a real network call.
type Upstream struct {
	base   *url.URL
	client *http.Client
}

// NewUpstream builds an Upstream that forwards every request to base
// (e.g. https://api.openai.com). client defaults to http.DefaultClient if
// nil.
func NewUpstream(base *url.URL, client *http.Client) *Upstream {
	if client == nil {
		client = http.DefaultClient
	}
	return &Upstream{base: base, client: client}
}

// Do sends req to the configured upstream. req.URL is expected to carry
// only a path and query, as an incoming server request does; Do overwrites
// its scheme and host in place before sending. Callers must pass a request
// built specifically for this call (e.g. via http.NewRequestWithContext),
// never one still referenced elsewhere, since its URL is mutated.
func (u *Upstream) Do(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = u.base.Scheme
	req.URL.Host = u.base.Host
	req.Host = u.base.Host
	return u.client.Do(req)
}
