package cassette

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"io"
	"net/http"
)

// Interactions represents a collection of interactions.
type Interactions []Interaction

// Interaction represents a single observed HTTP request/response pair.
type Interaction struct {
	Request  Request  `json:"request"`
	Response Response `json:"response,omitzero"`
}

// Request represents an observed HTTP request.
type Request struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers http.Header `json:"header,omitempty"`
	Body    Body        `json:"body,omitempty"`
}

// NewRequest creates a new [Request] out of an [*http.Request].
// If the request has a body, it is drained and restored.
func NewRequest(req *http.Request) (Request, error) {
	r := Request{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: req.Header.Clone(),
	}

	if req.Body == nil || req.Body == http.NoBody {
		return r, nil
	}

	// Drain and restore the request body.
	body, err := io.ReadAll(req.Body)
	req.Body.Close()

	if err != nil {
		return r, err
	}

	r.Body = body
	req.Body = io.NopCloser(bytes.NewReader(body))

	return r, nil
}

// Create creates a corresponding [*http.Request].
func (r Request) Create(ctx context.Context) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, func() io.Reader {
		if r.Body == nil {
			return nil
		}

		return bytes.NewReader(r.Body)
	}())
	if err != nil {
		return nil, err
	}

	if r.Headers != nil {
		req.Header = r.Headers.Clone()
	}

	return req, nil
}

// Response represents an observed HTTP response.
type Response struct {
	StatusCode int         `json:"statusCode"`
	Headers    http.Header `json:"header,omitempty"`
	Body       Body        `json:"body,omitempty"`
}

// NewResponse creates a new [Response] out of an [*http.Response].
// If the response has a body, it is drained and restored.
func NewResponse(resp *http.Response) (Response, error) {
	r := Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
	}

	// Drain and restore the response body.
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		return r, err
	}

	r.Body = body
	resp.Body = io.NopCloser(bytes.NewReader(body))

	return r, nil
}

type Body []byte

func (b Body) MarshalJSONTo(enc *jsontext.Encoder) error {
	if jsontext.Value(b).IsValid() {
		return enc.WriteValue(jsontext.Value(b))
	}

	return enc.WriteToken(jsontext.String(string(b)))
}

func (b *Body) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}

	*b = Body(val.Clone())

	return nil
}
