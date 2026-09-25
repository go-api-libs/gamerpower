package enrich

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/MarkRosemaker/openapi"
	"github.com/MarkRosemaker/openapi-enrich/cassette"
	merge "github.com/MarkRosemaker/openapi-merge"
)

const (
	schemeNameBearer = "bearerAuth"
	schemeNameBasic  = "BasicAuth"
)

func analyzeInteraction(doc *openapi.Document, ia *cassette.Interaction) error {
	reqURL, err := url.Parse(ia.Request.URL)
	if err != nil {
		return fmt.Errorf("parsing request URL: %w", err)
	}

	// 1. Initialize doc.Servers[0] from the first request if empty.
	// Use scheme+host only; path prefixes (e.g. versioning) are the caller's responsibility.
	if len(doc.Servers) == 0 {
		doc.Servers = openapi.Servers{{URL: reqURL.Scheme + "://" + reqURL.Host}}
	}

	// 2. Determine the effective server base URL for this request.
	// When the host matches the document-level server the request is at home.
	// When it differs, the path item will carry its own servers entry so clients
	// know which host to reach that path on.
	defaultURL, err := url.Parse(doc.Servers[0].URL)
	if err != nil {
		return fmt.Errorf("parsing server URL: %w", err)
	}

	reqServerURL := doc.Servers[0].URL
	altHost := reqURL.Host != defaultURL.Host
	if altHost {
		reqServerURL = reqURL.Scheme + "://" + reqURL.Host
	}

	// 3. Find or create the path item.
	matchedPath, pi := findPathItem(doc, reqURL)
	if pi == nil {
		// Detect ID-like segments and replace with {param} placeholders.
		paramPath, paramNames := newParametricPath(string(matchedPath))
		matchedPath = paramPath

		// Delete "/" placeholder if it exists and we're adding a real path.
		if matchedPath != "/" {
			delete(doc.Paths, "/")
		}

		pi = &openapi.PathItem{}

		if doc.Paths == nil {
			doc.Paths = openapi.Paths{}
		}

		doc.Paths.Set(matchedPath, pi)

		// Add path parameter definitions for each detected param.
		// Use the original (concrete) relative path for schema inference.
		if len(paramNames) > 0 {
			origPath := relativePath(reqURL, reqServerURL)
			addPathParams(pi, matchedPath, pathSegments(origPath), paramNames)
		}
	}

	// Stamp a path-level servers entry when the host differs from the document
	// server, so clients know where to send requests for this specific path.
	if altHost {
		altSrv := openapi.Server{URL: reqServerURL}
		found := false
		for _, s := range pi.Servers {
			if s.URL == altSrv.URL {
				found = true
				break
			}
		}

		if !found {
			pi.Servers = append(pi.Servers, altSrv)
		}
	}

	// 4. Find or create the operation for this HTTP method.
	op := getOrCreateOperation(pi, ia.Request.Method)

	// 5. Process query parameters.
	if err := processQueryParams(doc, pi, op, reqURL); err != nil {
		return fmt.Errorf("query params: %w", err)
	}

	// 6. Process request headers.
	var contentType string
	if err := processRequestHeaders(doc, pi.Parameters, op, ia.Request.Headers, &contentType); err != nil {
		return fmt.Errorf("request headers: %w", err)
	}

	// 7. Process request body.
	if len(ia.Request.Body) > 0 && isJSONMediaType(contentType) {
		if err := processRequestBody(op, ia.Request.Body, contentType); err != nil {
			return fmt.Errorf("request body: %w", err)
		}
	}

	// 8. Infer operation ID if not already set.
	if op.OperationID == "" {
		op.OperationID = inferOperationID(ia.Request.Method, matchedPath)
	}

	// 9. Process response.
	if err := processResponse(op, &ia.Response); err != nil {
		return fmt.Errorf("response: %w", err)
	}

	return nil
}

// getOrCreateOperation returns the existing operation for the given method
// or creates a new one and attaches it.
func getOrCreateOperation(pi *openapi.PathItem, method string) *openapi.Operation {
	for m, op := range pi.Operations {
		if strings.EqualFold(m, method) {
			return op
		}
	}

	op := &openapi.Operation{}
	pi.SetOperation(method, op)

	return op
}

func processQueryParams(doc *openapi.Document, pi *openapi.PathItem, op *openapi.Operation, reqURL *url.URL) error {
	query := reqURL.Query()
	for _, name := range slices.Sorted(maps.Keys(query)) {
		values := query[name]
		value := strings.Join(values, ",")

		var (
			schema       *openapi.Schema
			explodeFalse bool
		)

		if strings.Contains(value, ",") {
			// Comma-separated → non-exploded array
			items, err := commaListSchema(value)
			if err != nil {
				return fmt.Errorf("param %q: %w", name, err)
			}

			schema = &openapi.Schema{
				Type:  openapi.TypeArray,
				Items: &openapi.SchemaRef{Value: items},
			}
			explodeFalse = true
		} else {
			switch value {
			case "true", "false":
				schema = &openapi.Schema{
					Type:    openapi.TypeBoolean,
					Example: jsontext.Value(value),
				}
			default:
				var err error

				schema, err = scalarSchema(value)
				if err != nil {
					return fmt.Errorf("param %q: %w", name, err)
				}
			}
		}

		incoming := &openapi.Parameter{
			Name:   name,
			In:     openapi.ParameterLocationQuery,
			Schema: &openapi.SchemaRef{Value: schema},
		}
		if explodeFalse {
			incoming.Explode = new(false)
		}

		if existing := findParam(pi.Parameters, op.Parameters, name, openapi.ParameterLocationQuery); existing != nil {
			if err := merge.Parameter(existing, incoming); err != nil {
				return fmt.Errorf("merging path-level param %q: %w", name, err)
			}
			continue
		}

		op.Parameters = append(op.Parameters, &openapi.ParameterRef{Value: incoming})
	}

	return nil
}

func processRequestHeaders(doc *openapi.Document, piParams openapi.ParameterList, op *openapi.Operation, h http.Header, contentType *string) error {
	for _, key := range slices.Sorted(maps.Keys(h)) {
		// assume all keys are stored in canonical form
		if canonical := http.CanonicalHeaderKey(key); canonical != key {
			return fmt.Errorf("header %q: not in canonical form (%q)", key, canonical)
		}

		val := h.Get(key)

		switch key {
		case "Authorization":
			if err := processAuth(doc, op, val); err != nil {
				return fmt.Errorf("%s: %w", val, err)
			}
		case "Content-Type":
			mt, _, err := mime.ParseMediaType(val)
			if err != nil {
				return fmt.Errorf("parsing media type %q: %w", val, err)
			}

			*contentType = mt
		case "User-Agent", "Referer", "Cookie":
			// ignored
		default:
			if isCustomHeader(key) {
				if err := processCustomHeader(piParams, op, key, val); err != nil {
					return fmt.Errorf("header %q: %w", key, err)
				}
			}

			// Standard headers we don't model are silently skipped.
		}
	}

	return nil
}

func processAuth(doc *openapi.Document, op *openapi.Operation, v string) error {
	var scheme, schemeName string

	switch {
	case strings.HasPrefix(v, "Bearer "):
		scheme = openapi.SecuritySchemeBearer
		schemeName = schemeNameBearer
	case strings.HasPrefix(v, "Basic "):
		scheme = openapi.SecuritySchemeBasic
		schemeName = schemeNameBasic

		// Validate it is either masked or actually base64
		encoded := strings.TrimPrefix(v, "Basic ")
		if encoded != strings.Repeat("*", len(encoded)) {
			if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
				return fmt.Errorf("invalid basic auth: %w", err)
			}
		}
	default:
		return nil
	}

	// Add security scheme to components if not present.
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = openapi.SecuritySchemes{}
	}

	name := openapi.SecuritySchemeName(schemeName)
	if _, ok := doc.Components.SecuritySchemes[name]; !ok {
		doc.Components.SecuritySchemes.Set(name, &openapi.SecuritySchemeRef{
			Value: &openapi.SecurityScheme{
				Type:   openapi.SecuritySchemeTypeHTTP,
				Scheme: scheme,
			},
		})
	}

	// Add security requirement to operation if not present there
	// or in the general security settings.
	req := openapi.SecurityRequirement{name: []string{}}
	if !op.Security.Contains(req) && !doc.Security.Contains(req) {
		op.Security = append(op.Security, req)
	}

	return nil
}

func processCustomHeader(piParams openapi.ParameterList, op *openapi.Operation, name, value string) error {
	schema, err := scalarSchema(value)
	if err != nil {
		return err
	}

	incoming := &openapi.Parameter{
		Name:     name,
		In:       openapi.ParameterLocationHeader,
		Required: true,
		Schema:   &openapi.SchemaRef{Value: schema},
	}

	if existing := findParam(piParams, op.Parameters, name, openapi.ParameterLocationHeader); existing != nil {
		return merge.Parameter(existing, incoming)
	}

	op.Parameters = append(op.Parameters, &openapi.ParameterRef{Value: incoming})

	return nil
}

func processRequestBody(op *openapi.Operation, body []byte, contentType string) error {
	schema, err := newSchemaFromJSON(body)
	if err != nil {
		return err
	}

	mr := openapi.MediaRange(contentType)
	if op.RequestBody == nil {
		op.RequestBody = &openapi.RequestBodyRef{Value: &openapi.RequestBody{
			Required: true,
			Content:  openapi.Content{},
		}}
		op.RequestBody.Value.Content.Set(mr, &openapi.MediaType{
			Schema: &openapi.SchemaRef{Value: schema},
		})

		return nil
	}

	rb := op.RequestBody.Value
	if existing, ok := rb.Content[mr]; ok {
		if existing.Schema != nil {
			if err := merge.Schema(existing.Schema.Value, schema, false); err != nil {
				return err
			}

			return growEnums(existing.Schema.Value, body)
		}

		existing.Schema = &openapi.SchemaRef{Value: schema}

		return nil
	}

	rb.Content.Set(mr, &openapi.MediaType{Schema: &openapi.SchemaRef{Value: schema}})

	return nil
}

func processResponse(op *openapi.Operation, resp *cassette.Response) error {
	incoming, err := buildResponse(resp)
	if err != nil {
		return fmt.Errorf("building response: %w", err)
	}

	sc := openapi.StatusCode(strconv.Itoa(resp.StatusCode))

	if op.Responses == nil {
		op.Responses = openapi.OperationResponses{}
	}

	existing, ok := op.Responses[sc]
	if !ok {
		op.Responses.Set(sc, &openapi.ResponseRef{Value: incoming})
		return nil
	}

	if err := merge.Response(existing.Value, incoming); err != nil {
		return err
	}

	for _, mt := range existing.Value.Content {
		if mt.Schema == nil {
			continue
		}

		if err := growEnums(mt.Schema.Value, resp.Body); err != nil {
			return fmt.Errorf("content: %w", err)
		}
	}

	return nil
}

// findParam finds a parameter by name and location in a ParameterList.
func findParam(piParams, opParams openapi.ParameterList, name string, in openapi.ParameterLocation) *openapi.Parameter {
	for _, p := range append(opParams, piParams...) {
		if p.Value != nil && p.Value.Name == name && p.Value.In == in {
			if p.Value.Schema.Value.Type == "" {
				p.Value.Schema.Value.Type = openapi.TypeString
			}

			return p.Value
		}
	}

	return nil
}

// scalarSchema infers a schema from a single string value.
func scalarSchema(value string) (*openapi.Schema, error) {
	if _, err := strconv.Atoi(value); err == nil {
		return &openapi.Schema{Type: openapi.TypeInteger}, nil
	}

	return newSchemaFromJSON(fmt.Appendf(nil, "%q", value))
}

// commaListSchema infers the item schema from a comma-separated string.
func commaListSchema(value string) (*openapi.Schema, error) {
	parts := strings.Split(value, ",")

	var itemSchema *openapi.Schema
	for _, part := range parts {
		s, err := scalarSchema(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}

		if itemSchema == nil {
			itemSchema = s
		} else {
			if err := merge.Schema(itemSchema, s, false); err != nil {
				return nil, err
			}
		}
	}

	return itemSchema, nil
}

// standardRequestHeaders is the set of request headers that are not captured
// as parameters — they are either handled specially or not meaningful to model.
var standardRequestHeaders = map[string]struct{}{
	"Accept":              {},
	"Accept-Charset":      {},
	"Accept-Encoding":     {},
	"Accept-Language":     {},
	"Cache-Control":       {},
	"Connection":          {},
	"Host":                {},
	"If-Match":            {},
	"If-Modified-Since":   {},
	"If-None-Match":       {},
	"If-Unmodified-Since": {},
	"Origin":              {},
	"Pragma":              {},
	"Range":               {},
	"Te":                  {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// addPathParams adds Required path Parameter definitions to pi for each
// detected parameter name. The schema is inferred from the concrete segment value.
func addPathParams(pi *openapi.PathItem, p openapi.Path, reqSegments, paramNames []string) {
	pp := parsePath(string(p))
	for i, el := range pp {
		if !el.isParam {
			continue
		}

		// Look up the concrete segment value from the original request segments.
		var schema *openapi.Schema
		if i < len(reqSegments) {
			if _, err := strconv.Atoi(reqSegments[i]); err == nil {
				schema = &openapi.Schema{Type: openapi.TypeInteger}
			} else {
				schema = &openapi.Schema{Type: openapi.TypeString}
			}
		} else {
			schema = &openapi.Schema{Type: openapi.TypeString}
		}

		pi.Parameters = append(pi.Parameters, &openapi.ParameterRef{
			Value: &openapi.Parameter{
				Name:     el.name,
				In:       openapi.ParameterLocationPath,
				Required: true,
				Schema:   &openapi.SchemaRef{Value: schema},
			},
		})
	}
}

// isCustomHeader reports whether a canonical header name should be captured
// as a parameter — X-* vendor headers or any unknown non-standard header.
func isCustomHeader(key string) bool {
	if strings.HasPrefix(key, "X-") {
		return true
	}

	_, isStd := standardRequestHeaders[key]

	return !isStd
}
