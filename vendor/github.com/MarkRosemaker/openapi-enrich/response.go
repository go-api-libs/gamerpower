package enrich

import (
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/MarkRosemaker/openapi"
	"github.com/MarkRosemaker/openapi-enrich/cassette"
)

// buildResponse constructs an openapi.Response from an observed HTTP response.
func buildResponse(resp *cassette.Response) (*openapi.Response, error) {
	description := http.StatusText(resp.StatusCode)
	if description == "" {
		description = "response"
	}

	r := &openapi.Response{Description: description}

	ct := resp.Headers.Get("Content-Type")
	if ct == "" || len(resp.Body) == 0 {
		return r, nil
	}

	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return r, nil
	}

	switch {
	case isJSONMediaType(mediaType):
		schema, err := newSchemaFromJSON(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("creating schema from JSON: %w", err)
		}

		r.Content = openapi.Content{}
		r.Content.Set(openapi.MediaRange(mediaType), &openapi.MediaType{
			Schema: &openapi.SchemaRef{Value: schema},
		})

	case mediaType == "text/plain":
		bodyStr := strings.TrimSpace(string(resp.Body))
		if !strings.EqualFold(bodyStr, description) {
			r.Content = openapi.Content{}
			r.Content.Set(openapi.MediaRange(mediaType), &openapi.MediaType{
				Schema: &openapi.SchemaRef{Value: &openapi.Schema{Type: openapi.TypeString}},
			})
		}

	case mediaType == "text/html":
		r.Content = openapi.Content{}
		r.Content.Set(openapi.MediaRange(mediaType), &openapi.MediaType{})
	}

	return r, nil
}

func isJSONMediaType(mediaType string) bool {
	return mediaType == "application/json" ||
		strings.HasSuffix(mediaType, "+json") ||
		strings.Contains(mediaType, "/json")
}

// infraResponseHeaders is the set of response headers to skip when building
// a response schema — CDN, caching, security, and other infrastructure headers.
var infraResponseHeaders = map[string]struct{}{
	"accept-ranges":                     {},
	"access-control-allow-credentials":  {},
	"access-control-allow-headers":      {},
	"access-control-allow-methods":      {},
	"access-control-allow-origin":       {},
	"access-control-expose-headers":     {},
	"access-control-max-age":            {},
	"age":                               {},
	"alt-svc":                           {},
	"cache-control":                     {},
	"cdn-cache-control":                 {},
	"cf-cache-status":                   {},
	"cf-ray":                            {},
	"connection":                        {},
	"content-encoding":                  {},
	"content-length":                    {},
	"content-security-policy":           {},
	"content-type":                      {},
	"date":                              {},
	"etag":                              {},
	"expires":                           {},
	"last-modified":                     {},
	"nel":                               {},
	"permissions-policy":                {},
	"pragma":                            {},
	"referrer-policy":                   {},
	"report-to":                         {},
	"server":                            {},
	"server-timing":                     {},
	"set-cookie":                        {},
	"strict-transport-security":         {},
	"timing-allow-origin":               {},
	"transfer-encoding":                 {},
	"vary":                              {},
	"via":                               {},
	"x-cache":                           {},
	"x-cache-hits":                      {},
	"x-content-type-options":            {},
	"x-dns-prefetch-control":            {},
	"x-download-options":                {},
	"x-envoy-upstream-service-time":     {},
	"x-frame-options":                   {},
	"x-permitted-cross-domain-policies": {},
	"x-powered-by":                      {},
	"x-ratelimit-limit":                 {},
	"x-ratelimit-remaining":             {},
	"x-ratelimit-reset":                 {},
	"x-request-id":                      {},
	"x-response-time":                   {},
	"x-runtime":                         {},
	"x-served-by":                       {},
	"x-timer":                           {},
	"x-xss-protection":                  {},
}

func isInfraResponseHeader(name string) bool {
	_, ok := infraResponseHeaders[strings.ToLower(name)]
	return ok
}
