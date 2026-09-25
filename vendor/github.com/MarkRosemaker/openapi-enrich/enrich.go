// Package enrich enriches an OpenAPI document from observed HTTP interactions.
// It infers paths, operations, parameters, request bodies, and response schemas.
// Schemas are left inline; the caller may flatten, tidy, or sort afterward.
package enrich

import (
	"fmt"

	"github.com/MarkRosemaker/openapi"
	"github.com/MarkRosemaker/openapi-enrich/cassette"
)

// Enrich updates doc in place based on observed HTTP interactions.
// It adds paths, operations, parameters, request bodies, and response schemas
// inferred from the interactions. Schemas are left inline — the caller can
// flatten (openapi-flatten), tidy (operation IDs, security hoisting), or
// sort component maps afterward as desired.
func Enrich(doc *openapi.Document, interactions cassette.Interactions) error {
	for _, ia := range interactions {
		if err := analyzeInteraction(doc, &ia); err != nil {
			return fmt.Errorf("%s %s: %w", ia.Request.Method, ia.Request.URL, err)
		}
	}

	hoistSecurity(doc)

	return nil
}
