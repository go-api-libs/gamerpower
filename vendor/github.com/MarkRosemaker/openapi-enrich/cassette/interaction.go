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
	// BodyOmitted is set for a body that was not text, see [IsText], which is not recorded.
	BodyOmitted bool `json:"bodyOmitted,omitzero"`
}

// NewRequest creates a new [Request] out of an [*http.Request].
// If the request has a body of text, it is drained and restored; any other is left as it is, and recorded as omitted.
func NewRequest(req *http.Request) (Request, error) {
	r := Request{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: req.Header.Clone(),
	}

	if req.Body == nil || req.Body == http.NoBody {
		return r, nil
	}

	if !IsText(req.Header.Get("Content-Type")) {
		r.BodyOmitted = true
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
	// BodyOmitted is set for a body that was not text, see [IsText], which is not recorded.
	BodyOmitted bool `json:"bodyOmitted,omitzero"`
}

// NewResponse creates a new [Response] out of an [*http.Response].
// If the response has a body of text, it is drained and restored; any other, such as a zip, is left to stream as it
// is, and recorded as omitted.
func NewResponse(resp *http.Response) (Response, error) {
	r := Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
	}

	if !IsText(resp.Header.Get("Content-Type")) {
		r.BodyOmitted = resp.Body != nil && resp.Body != http.NoBody
		return r, nil
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

// Body is a recorded body: written as the JSON it is, or as a string if it is no JSON.
type Body []byte

// MarshalJSONTo implements [json.MarshalerTo].
func (b Body) MarshalJSONTo(enc *jsontext.Encoder) error {
	if jsontext.Value(b).IsValid() {
		return enc.WriteValue(jsontext.Value(b))
	}

	return enc.WriteToken(jsontext.String(string(b)))
}

// UnmarshalJSONFrom implements [json.UnmarshalerFrom].
func (b *Body) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}

	*b = Body(val.Clone())

	return nil
}
