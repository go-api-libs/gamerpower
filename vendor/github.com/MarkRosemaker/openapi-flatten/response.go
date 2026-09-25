package flatten

import (
	"strings"

	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
	"github.com/ettle/strcase"
)

// nameResponse returns a human-readable name for the response.
func nameResponse(opID string, code openapi.StatusCode) string {
	statusText := code.StatusText()
	if statusText == "" {
		statusText = string(code)
	}

	return strcase.ToGoPascal(strings.Join([]string{opID, statusText, "Response"}, " "))
}

func response(d *openapi.Document, r *openapi.Response, rspName string, modeSchema mode) error {
	// if err := l.resolveHeaders(r.Headers); err != nil {
	// 	return &errpath.ErrField{Field: "headers", Err: err}
	// }
	if err := content(d, r.Content, rspName, "Response", modeSchema); err != nil {
		return &errpath.ErrField{Field: "content", Err: err}
	}

	// if err := l.resolveLinks(r.Links); err != nil {
	// 	return &errpath.ErrField{Field: "links", Err: err}
	// }

	return nil
}

func responseRef(d *openapi.Document, r *openapi.ResponseRef, rspName string, modeSchema mode) error {
	if r.Ref != nil {
		return nil
	}

	// reference the response in the components
	rspName = uniqueName(d.Components.Responses, rspName)
	d.Components.Responses.Set(rspName, &openapi.ResponseRef{Value: r.Value})
	r.Ref = newRef("responses", rspName)

	return response(d, r.Value, rspName, modeSchema) // flatten the response itself
}
