package recorder

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/MarkRosemaker/openapi-enrich/cassette"
)

// Transport is an [http.RoundTripper] that records HTTP exchanges as
// [Interaction] values and replays cached responses for repeated requests,
// acting like a VCR cassette.
//
// The first time a request is seen it is forwarded to the underlying transport,
// the response is stored in an internal cache, and the interaction is appended
// to [Transport.Interactions]. Subsequent identical requests are served
// directly from the cache — no network call is made — and the replayed
// interaction is still appended so Interactions contains the full call history
// in order.
//
// Two requests are considered identical when their method, URL, and body
// produce the same [requestKey]. Request headers are intentionally excluded
// from the key: the same endpoint should replay the same response regardless
// of which auth token or version header was used.
//
// Transport is not safe for concurrent use.
type Transport struct {
	// Transport is the underlying RoundTripper used for cache misses.
	// Defaults to [http.DefaultTransport] when nil.
	Transport http.RoundTripper

	// Interactions contains every exchange in call order, excluding replays.
	Interactions cassette.Interactions

	cache map[string]cassette.Response // keyed by requestKey
}

func NewTransport(transport http.RoundTripper, interactions cassette.Interactions) *Transport {
	cache := map[string]cassette.Response{}

	ias := cassette.Interactions{}
	for _, ia := range interactions {
		if ia.Response.StatusCode > 0 {
			cache[requestKey(ia.Request)] = ia.Response
			ias = append(ias, ia)
		}
	}

	return &Transport{
		Transport:    transport,
		Interactions: ias,
		cache:        cache,
	}
}

// RoundTrip implements [http.RoundTripper].
//
// On a cache hit the stored response is returned immediately without touching
// the network. On a cache miss the request is forwarded to the underlying
// transport, the response is cached, and both cases append to
// [Transport.Interactions].
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	cReq, err := cassette.NewRequest(req)
	if err != nil {
		return nil, err
	}

	key := requestKey(cReq)

	if cached, ok := t.cache[key]; ok {
		return responseFromCached(cached), nil
	}

	resp, err := t.underlying().RoundTrip(req)
	if err != nil {
		return nil, err
	}

	cResp, err := cassette.NewResponse(resp)
	if err != nil {
		return nil, err
	}

	t.Interactions = append(t.Interactions, cassette.Interaction{
		Request:  cReq,
		Response: cResp,
	})

	if t.cache == nil {
		t.cache = map[string]cassette.Response{}
	}

	t.cache[key] = cResp

	return resp, nil
}

func (t *Transport) underlying() http.RoundTripper {
	if t.Transport != nil {
		return t.Transport
	}

	return &defaultOrInsecureTransport{}
}

type defaultOrInsecureTransport struct{}

var insecureTransport = &http.Transport{
	// self-signed cert; safe because it's localhost only
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}

func (c *defaultOrInsecureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() == "localhost" {
		return insecureTransport.RoundTrip(req)
	}

	return http.DefaultTransport.RoundTrip(req)
}

// requestKey returns a string that uniquely identifies a request by its
// method, URL, and body. Headers are excluded so the same logical endpoint
// replays the same response regardless of auth or version headers.
//
// The body is represented as its SHA-256 hex digest, appended after a newline.
// An empty or nil body produces no suffix, keeping keys readable in logs.
func requestKey(r cassette.Request) string {
	b := strings.Builder{}
	b.WriteString(r.Method)
	b.WriteByte(' ')

	// Ensure query parameters have the same order
	u, _ := url.Parse(r.URL)
	u.RawQuery = u.Query().Encode()
	b.WriteString(u.String())

	if len(r.Body) > 0 {
		h := sha256.Sum256(r.Body)

		b.WriteByte('\n')
		b.WriteString(hex.EncodeToString(h[:]))
	}

	if len(r.Headers) > 0 {
		h := sha256.New()
		keys := slices.Collect(maps.Keys(r.Headers))
		slices.Sort(keys)

		for _, k := range keys {
			if k == "Authorization" {
				continue
			}

			vals := r.Headers[k]
			if len(vals) == 0 {
				continue
			}

			fmt.Fprintf(h, "%s: %s", k, vals[0]) //nolint:errcheck
		}

		b.WriteByte('\n')
		b.WriteString(string(h.Sum(nil)))
	}

	return b.String()
}

// responseFromCached rebuilds an *http.Response from a stored [Response].
func responseFromCached(r cassette.Response) *http.Response {
	return &http.Response{
		StatusCode: r.StatusCode,
		Header:     r.Headers.Clone(),
		Body:       io.NopCloser(bytes.NewReader(r.Body)),
	}
}
