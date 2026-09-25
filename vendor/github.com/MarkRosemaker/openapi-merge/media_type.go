package merge

import (
	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

func MediaType(a, b *openapi.MediaType) error {
	if b.Schema != nil {
		if a.Schema != nil {
			if err := Schema(a.Schema.Value, b.Schema.Value, false); err != nil {
				return &errpath.ErrField{Field: "schema", Err: err}
			}
		} else {
			a.Schema = b.Schema
		}
	}

	// set the example of b
	if a.Example == nil {
		a.Example = b.Example
	}

	// if err := mt.Examples.Validate(); err != nil {
	// 	return &errpath.ErrField{Field: "examples", Err: err}
	// }

	// if err := mt.Encoding.Validate(); err != nil {
	// 	return &errpath.ErrField{Field: "encoding", Err: err}
	// }

	return extensions(a.Extensions, b.Extensions)
}
