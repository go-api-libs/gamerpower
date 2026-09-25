package flatten

import (
	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

func components(d *openapi.Document, c openapi.Components) error {
	if err := schemas(d, c.Schemas); err != nil {
		return &errpath.ErrField{Field: "schemas", Err: err}
	}

	if err := responses(d, c.Responses); err != nil {
		return &errpath.ErrField{Field: "responses", Err: err}
	}

	if err := parameters(d, c.Parameters); err != nil {
		return &errpath.ErrField{Field: "parameters", Err: err}
	}

	// if err := l.resolveExamples(c.Examples); err != nil {
	// 	return &errpath.ErrField{Field: "examples", Err: err}
	// }

	if err := requestBodies(d, c.RequestBodies); err != nil {
		return &errpath.ErrField{Field: "requestBodies", Err: err}
	}

	// if err := l.resolveHeaders(c.Headers); err != nil {
	// 	return &errpath.ErrField{Field: "headers", Err: err}
	// }

	// if err := l.resolveSecuritySchemes(c.SecuritySchemes); err != nil {
	// 	return &errpath.ErrField{Field: "securitySchemes", Err: err}
	// }

	// if err := l.resolveLinks(c.Links); err != nil {
	// 	return &errpath.ErrField{Field: "links", Err: err}
	// }

	// if err := l.resolveCallbackRefs(c.Callbacks); err != nil {
	// 	return &errpath.ErrField{Field: "callbacks", Err: err}
	// }

	// if err := l.resolvePathItems(c.PathItems); err != nil {
	// 	return &errpath.ErrField{Field: "pathItems", Err: err}
	// }

	return nil
}
