package edit

import (
	"encoding/json/v2"
	"slices"

	"github.com/MarkRosemaker/openapi"
)

// RedirectSchema repoints every reference to oldName at newName and removes
// oldName from components.schemas. It does not read or change either
// schema's own definition — newName's shape is left exactly as it was, and
// oldName's is discarded along with oldName itself, not merged into
// newName's. Combining two schemas' definitions into one wider shape is a
// distinct, unrelated operation; see [openapi-merge] for that.
//
// It differs from [RenameSchema], which refuses to rename a schema onto a
// name that already exists ([ErrSchemaExists]): redirecting onto an existing
// schema is exactly the point here, typically because several near-duplicate
// schemas (e.g. ones an OpenAPI generator produced one per endpoint, that
// happen to describe the same thing) are being consolidated onto one of
// them.
//
// If description is non-empty, it becomes the $ref-level description on
// every reference this repoints, replacing whatever description that
// reference already had. The usual reason to set it is that oldName's own
// definition — its bounds, its wording — is about to be discarded once
// oldName is gone; setting description is how that information survives on
// the references that used it, rather than being lost along with oldName.
//
// A oneOf or anyOf that listed both schemas lists newName twice afterwards, so
// it keeps only the first of those plain references: a value matching one
// would match the other, and a oneOf could never hold for it.
//
// It fails, changing nothing, if oldName or newName is not in
// components.schemas ([ErrSchemaNotFound]).
//
// [openapi-merge]: https://github.com/MarkRosemaker/openapi-merge
func RedirectSchema(doc *openapi.Document, oldName, newName, description string) error {
	schemas := doc.Components.Schemas

	if _, ok := schemas[oldName]; !ok {
		return &ErrSchemaNotFound{Name: oldName}
	}

	if _, ok := schemas[newName]; !ok {
		return &ErrSchemaNotFound{Name: newName}
	}

	if oldName == newName {
		return nil
	}

	keepImplicitMappings(doc, schemas[oldName], oldName, newName)

	old, new := schemaRefPrefix+oldName, schemaRefPrefix+newName

	target := schemas[newName]

	repointed := map[*openapi.Schema]bool{}

	walkSchemas(doc, func(s *openapi.Schema) {
		if s.Ref == nil || s.Ref.Identifier != old {
			return
		}

		if description != "" {
			s.Description = description
		}

		s.Ref.Identifier, s.Ref.Value = new, target
		repointed[s] = true
	})

	walkSchemas(doc, func(s *openapi.Schema) {
		s.OneOf = dropDuplicateAlternatives(s.OneOf, new, repointed)
		s.AnyOf = dropDuplicateAlternatives(s.AnyOf, new, repointed)
	})

	rewriteMappings(doc, oldName, newName)

	delete(schemas, oldName)

	return nil
}

// dropDuplicateAlternatives keeps only the first of a union's alternatives that refer to ref and nothing else, once
// redirecting has made more than one of them do so.
//
// Two alternatives of the same schema are no alternative at all: a value that matches one matches the other, so a
// oneOf could never hold for it.
func dropDuplicateAlternatives(alts openapi.SchemaList, ref string, repointed map[*openapi.Schema]bool) openapi.SchemaList {
	isRef := func(a *openapi.Schema) bool {
		if a.Ref == nil || a.Ref.Identifier != ref {
			return false
		}

		c := *a
		c.Ref, c.Title, c.Description = nil, "", ""
		b, err := json.Marshal(&c)

		return err == nil && string(b) == "{}"
	}

	if !slices.ContainsFunc(alts, func(a *openapi.Schema) bool { return repointed[a] }) {
		return alts
	}

	i := slices.IndexFunc(alts, isRef)
	if i < 0 {
		return alts
	}

	first := alts[i]

	return slices.DeleteFunc(alts, func(a *openapi.Schema) bool { return a != first && isRef(a) })
}
