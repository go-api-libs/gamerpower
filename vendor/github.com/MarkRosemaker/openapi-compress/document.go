package compress

import (
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/MarkRosemaker/openapi"
	"github.com/MarkRosemaker/openapi-compare/schema"
	edit "github.com/MarkRosemaker/openapi-edit"
)

// Document compresses an OpenAPI document so it contains no duplicate schemas.
// It does so by merging schemas that have the same shape (see schema.SameShape) -
// documentation-only differences like title or description don't prevent a merge.
// Furthermore, schemas with significant overlap are merged according to cfg.
// After compression, long names of merged schemas are shortened.
func Document(d *openapi.Document, cfg Config) error {
	cfg.setDefaults()

	if err := cfg.validate(); err != nil {
		return err
	}

	deduplicateParameters(d)

	// Step down from exact equality to MinSimilarity, running each threshold
	// until stable before moving to the next.
	mergedCanonicals := map[string]bool{}
	threshold := 1.0
	for {
		for {
			canonicals, err := deduplicateSchemasAtThreshold(d, threshold)
			if err != nil {
				return err
			}

			if len(canonicals) == 0 {
				break
			}

			for name := range canonicals {
				mergedCanonicals[name] = true
			}
		}

		if threshold <= cfg.MinSimilarity+1e-9 {
			break
		}

		threshold = math.Max(cfg.MinSimilarity, threshold-cfg.SimilarityStep)
	}

	if !cfg.SkipNameShortening {
		if err := shortenMergedSchemaNames(d, mergedCanonicals); err != nil {
			return err
		}
	}

	if cfg.TrimExamples > 0 {
		if err := edit.TrimSchemaExamples(d, cfg.TrimExamples); err != nil {
			return err
		}
	}

	return nil
}

// deduplicateSchemasAtThreshold performs one dedup pass at the given similarity
// threshold.  It returns the set of canonical schema names that had at least one
// other schema merged into them (empty map means nothing was merged).
func deduplicateSchemasAtThreshold(d *openapi.Document, threshold float64) (map[string]bool, error) {
	schemas := d.Components.Schemas
	if len(schemas) < 2 {
		return nil, nil
	}

	names := sortedSchemaNames(schemas)

	// replacements maps a name-to-remove to its canonical name.
	replacements := map[string]string{}

	for i, nameA := range names {
		if _, removed := replacements[nameA]; removed {
			continue
		}

		schemaA := schemas[nameA]
		for _, nameB := range names[i+1:] {
			if _, removed := replacements[nameB]; removed {
				continue
			}

			schemaB := schemas[nameB]

			var sim float64
			if threshold >= 1.0 {
				// Fast path: same shape, ignoring documentation-only differences.
				if schema.SameShape(schemaA, schemaB) {
					sim = 1.0
				}
			} else {
				// Size bound: max possible Jaccard = min(|a|,|b|) / max(|a|,|b|).
				// If that bound is already below the threshold, skip the expensive
				// similarity computation.
				pa, pb := len(schemaA.Properties), len(schemaB.Properties)
				if pa > 0 && pb > 0 {
					lo, hi := pa, pb
					if lo > hi {
						lo, hi = hi, lo
					}

					if float64(lo)/float64(hi) < threshold {
						continue
					}
				}

				sim = schemasSimilarity(schemaA, schemaB)
			}

			if sim < threshold {
				continue
			}

			if sim < 1.0 {
				// Not exactly equal: widen schemaA to also cover schemaB.
				mergeSchemas(schemaA, schemaB)
			}

			replacements[nameB] = nameA
		}
	}

	if len(replacements) == 0 {
		return nil, nil
	}

	// Collect canonical names (the values in replacements).
	canonicals := make(map[string]bool, len(replacements))
	for _, canonical := range replacements {
		canonicals[canonical] = true
	}

	for name := range replacements {
		delete(d.Components.Schemas, name)
	}

	replaceRefsInDocument(d, replacements)

	return canonicals, nil
}

// deduplicateParameters removes exact duplicate parameter definitions from
// d.Components.Parameters, keeping the alphabetically-first name as canonical
// and updating all $ref identifiers throughout the document.
func deduplicateParameters(d *openapi.Document) {
	params := d.Components.Parameters
	if len(params) < 2 {
		return
	}

	names := sortedParameterNames(params)
	replacements := map[string]string{}

	for i, nameA := range names {
		if _, removed := replacements[nameA]; removed {
			continue
		}

		refA := params[nameA]
		if refA == nil || refA.Value == nil {
			continue
		}

		for _, nameB := range names[i+1:] {
			if _, removed := replacements[nameB]; removed {
				continue
			}

			refB := params[nameB]
			if refB == nil || refB.Value == nil {
				continue
			}

			if reflect.DeepEqual(refA.Value, refB.Value) {
				replacements[nameB] = nameA
			}
		}
	}

	if len(replacements) == 0 {
		return
	}

	for name := range replacements {
		delete(d.Components.Parameters, name)
	}

	replaceParameterRefsInDocument(d, replacements)
}

func sortedParameterNames(params openapi.Parameters) []string {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// replaceParameterRefsInDocument updates $ref identifiers for parameters
// throughout the entire document.
func replaceParameterRefsInDocument(d *openapi.Document, replacements map[string]string) {
	for _, p := range d.Paths {
		replaceParameterRefsInPathItem(p, replacements)
	}

	for _, piRef := range d.Webhooks {
		if piRef != nil && piRef.Value != nil {
			replaceParameterRefsInPathItem(piRef.Value, replacements)
		}
	}

	for _, piRef := range d.Components.PathItems {
		if piRef != nil && piRef.Value != nil {
			replaceParameterRefsInPathItem(piRef.Value, replacements)
		}
	}
}

func replaceParameterRefsInPathItem(p *openapi.PathItem, replacements map[string]string) {
	if p == nil {
		return
	}

	replaceParameterRefList(p.Parameters, replacements)

	for _, op := range p.Operations {
		if op != nil {
			replaceParameterRefList(op.Parameters, replacements)
		}
	}
}

func replaceParameterRefList(params openapi.ParameterList, replacements map[string]string) {
	for _, p := range params {
		if p == nil || p.Ref == nil {
			continue
		}

		name := parameterNameFromRef(p.Ref.Identifier)
		if canonical, ok := replacements[name]; ok {
			p.Ref.Identifier = "#/components/parameters/" + canonical
		}
	}
}

func parameterNameFromRef(identifier string) string {
	const prefix = "#/components/parameters/"
	return strings.TrimPrefix(identifier, prefix)
}

// replaceRefsInDocument updates $ref identifiers throughout the entire document.
func replaceRefsInDocument(d *openapi.Document, replacements map[string]string) {
	replaceRefsInComponents(&d.Components, replacements)

	for _, p := range d.Paths {
		replaceRefsInPathItem(p, replacements)
	}

	for _, piRef := range d.Webhooks {
		if piRef != nil && piRef.Value != nil {
			replaceRefsInPathItem(piRef.Value, replacements)
		}
	}

	for _, piRef := range d.Components.PathItems {
		if piRef != nil && piRef.Value != nil {
			replaceRefsInPathItem(piRef.Value, replacements)
		}
	}
}

func replaceRefsInPathItem(p *openapi.PathItem, replacements map[string]string) {
	if p == nil {
		return
	}

	replaceRefsInParameterList(p.Parameters, replacements)

	for _, op := range p.Operations {
		replaceRefsInOperation(op, replacements)
	}
}

func replaceRefsInOperation(op *openapi.Operation, replacements map[string]string) {
	if op == nil {
		return
	}

	replaceRefsInParameterList(op.Parameters, replacements)

	if op.RequestBody != nil && op.RequestBody.Value != nil {
		replaceRefsInContent(op.RequestBody.Value.Content, replacements)
	}

	for _, resp := range op.Responses {
		replaceRefsInResponseRef(resp, replacements)
	}

	for _, cb := range op.Callbacks {
		for _, piRef := range cb {
			if piRef != nil && piRef.Value != nil {
				replaceRefsInPathItem(piRef.Value, replacements)
			}
		}
	}
}

func replaceRefsInParameterList(params openapi.ParameterList, replacements map[string]string) {
	for _, p := range params {
		if p != nil && p.Value != nil {
			replaceSchemaRef(p.Value.Schema, replacements)
			replaceRefsInContent(p.Value.Content, replacements)
		}
	}
}

func replaceRefsInResponseRef(r *openapi.ResponseRef, replacements map[string]string) {
	if r == nil || r.Value == nil {
		return
	}

	replaceRefsInContent(r.Value.Content, replacements)

	for _, h := range r.Value.Headers {
		if h != nil && h.Value != nil {
			replaceRefsInSchema(h.Value.Schema, replacements)
			replaceRefsInContent(h.Value.Content, replacements)
		}
	}
}

// replaceRefsInComponents updates $ref identifiers throughout all components.
func replaceRefsInComponents(c *openapi.Components, replacements map[string]string) {
	replaceRefsInSchemas(c.Schemas, replacements)

	for _, ref := range c.Responses {
		replaceRefsInResponseRef(ref, replacements)
	}

	for _, ref := range c.RequestBodies {
		if ref != nil && ref.Value != nil {
			replaceRefsInContent(ref.Value.Content, replacements)
		}
	}

	for _, ref := range c.Parameters {
		if ref != nil && ref.Value != nil {
			replaceSchemaRef(ref.Value.Schema, replacements)
			replaceRefsInContent(ref.Value.Content, replacements)
		}
	}

	for _, ref := range c.Headers {
		if ref != nil && ref.Value != nil {
			replaceRefsInSchema(ref.Value.Schema, replacements)
			replaceRefsInContent(ref.Value.Content, replacements)
		}
	}
}

// replaceRefsInSchemas updates $ref identifiers within component schemas.
func replaceRefsInSchemas(schemas openapi.Schemas, replacements map[string]string) {
	for _, s := range schemas {
		replaceRefsInSchema(s, replacements)
	}
}

func replaceRefsInContent(content openapi.Content, replacements map[string]string) {
	for _, mt := range content {
		if mt != nil && mt.Schema != nil {
			replaceSchemaRef(mt.Schema, replacements)
			replaceRefsInSchema(mt.Schema.Value, replacements)
		}
	}
}

func replaceRefsInSchema(s *openapi.Schema, replacements map[string]string) {
	replaceRefsInSchemaRec(s, map[*openapi.Schema]bool{}, replacements)
}

func replaceRefsInSchemaRec(s *openapi.Schema, visited map[*openapi.Schema]bool, replacements map[string]string) {
	if s == nil || visited[s] {
		return
	}

	visited[s] = true

	for _, ref := range s.Properties {
		replaceSchemaRef(ref, replacements)

		if ref != nil && ref.Ref == nil {
			replaceRefsInSchemaRec(ref.Value, visited, replacements)
		}
	}

	if s.Items != nil {
		replaceSchemaRef(s.Items, replacements)

		if s.Items.Ref == nil {
			replaceRefsInSchemaRec(s.Items.Value, visited, replacements)
		}
	}

	if s.AdditionalProperties != nil {
		replaceSchemaRef(s.AdditionalProperties, replacements)

		if s.AdditionalProperties.Ref == nil {
			replaceRefsInSchemaRec(s.AdditionalProperties.Value, visited, replacements)
		}
	}

	for _, ref := range s.AllOf {
		replaceSchemaRef(ref, replacements)

		if ref != nil && ref.Ref == nil {
			replaceRefsInSchemaRec(ref.Value, visited, replacements)
		}
	}
}

func replaceSchemaRef(ref *openapi.SchemaRef, replacements map[string]string) {
	if ref == nil || ref.Ref == nil {
		return
	}

	name := schemaNameFromRef(ref.Ref.Identifier)
	if canonical, ok := replacements[name]; ok {
		ref.Ref.Identifier = "#/components/schemas/" + canonical
	}
}

// schemaNameFromRef extracts the schema name from a $ref like "#/components/schemas/Name".
func schemaNameFromRef(identifier string) string {
	const prefix = "#/components/schemas/"
	return strings.TrimPrefix(identifier, prefix)
}

func sortedSchemaNames(schemas openapi.Schemas) []string {
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}
