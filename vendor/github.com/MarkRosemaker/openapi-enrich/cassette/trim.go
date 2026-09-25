package cassette

import (
	"encoding/json/jsontext"

	edit "github.com/MarkRosemaker/openapi-edit"
)

// TrimResponseHeaders removes response headers we don't need in place.
func (ias Interactions) TrimResponseHeaders() {
	for _, ia := range ias {
		for k := range ia.Response.Headers {
			switch k {
			case "Content-Type": // leave
			default:
				delete(ia.Response.Headers, k)
			}
		}
	}
}

// TrimResponseBodies cuts every response body's own arrays down to at most
// maxItems representative elements, at any depth -- see
// [github.com/MarkRosemaker/openapi-edit.TrimExample]. A body that is empty
// or not valid JSON is left as it is.
func (ias Interactions) TrimResponseBodies(maxItems int) {
	for i, ia := range ias {
		if len(ia.Response.Body) == 0 {
			continue
		}

		trimmed, err := edit.TrimExample(jsontext.Value(ia.Response.Body), maxItems)
		if err != nil {
			continue // not valid JSON; leave the body as it is
		}

		ias[i].Response.Body = Body(trimmed)
	}
}
