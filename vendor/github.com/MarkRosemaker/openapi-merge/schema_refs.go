package merge

import (
	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

func SchemaRefs(a *openapi.SchemaRefs, b openapi.SchemaRefs) error {
	return schemaRefs(*a, a, b)
}

func schemaRefs(aAll openapi.SchemaRefs, aToSet *openapi.SchemaRefs, b openapi.SchemaRefs) error {
	for keyB, sB := range b.ByIndex() {
		sA, ok := aAll[keyB]
		if !ok {
			aToSet.Set(keyB, sB) // add the property
			continue
		}

		// merge the properties
		if err := Schema(sA.Value, sB.Value, false); err != nil {
			return &errpath.ErrKey{Key: keyB, Err: err}
		}
	}

	return nil
}
