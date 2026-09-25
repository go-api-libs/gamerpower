package flatten

import (
	"strings"

	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
	"github.com/ettle/strcase"
)

// nameMediaType returns a human-readable name for the media type.
func nameMediaType(rspOrReqBodyName, nameMediaRange string,
	// "Response" or "RequestBody"
	tp string,
) string {
	return strcase.ToGoPascal(strings.Join([]string{
		strings.TrimSuffix(rspOrReqBodyName, tp),
		// NOTE: We used to add nameMediaRange and tp in this []string
		// We need to find a way to include it if and only if there is ambiguity
	}, " "))
}

func mediaType(d *openapi.Document, mt *openapi.MediaType, mtName string, modeSchema mode) error {
	if mt.Schema != nil {
		if title := mt.Schema.Value.Title; title != "" {
			mtName = strcase.ToGoPascal(title)
		}

		if err := schemaRef(d, mt.Schema, mtName, modeSchema); err != nil {
			return &errpath.ErrField{Field: "schema", Err: err}
		}
	}

	// if err := l.resolveExamples(mt.Examples); err != nil {
	// 	return &errpath.ErrField{Field: "examples", Err: err}
	// }

	// if err := l.resolveEncodings(mt.Encoding); err != nil {
	// 	return &errpath.ErrField{Field: "encoding", Err: err}
	// }

	return nil
}
