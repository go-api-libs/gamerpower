package flatten

import (
	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

// Document flattens an entire OpenAPI document so it contains no nested objects.
func Document(d *openapi.Document) error {
	moveCommonPathPrefix(d)

	if err := paths(d, d.Paths); err != nil {
		return &errpath.ErrField{Field: "paths", Err: err}
	}

	// if err := webhooks(d.Webhooks); err != nil {
	// 	return &errpath.ErrField{Field: "webhooks", Err: err}
	// }

	if err := components(d, d.Components); err != nil {
		return &errpath.ErrField{Field: "components", Err: err}
	}

	hoistParams(d)

	return nil
}
