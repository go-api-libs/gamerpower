// Package edit provides structural edits to an OpenAPI document — changes
// where touching one place obliges you to touch several others.
package edit

import (
	"fmt"
	"regexp"

	"github.com/MarkRosemaker/openapi"
)

// schemaRefPrefix is the start of every reference to a component schema.
const schemaRefPrefix = "#/components/schemas/"

// reComponentKey is the pattern the OpenAPI specification requires of keys
// under components.
//
// It matters here beyond validity: a name containing "/" would produce a
// reference that resolves somewhere else entirely, and one containing a space
// would produce a reference that does not resolve at all.
//
// See https://spec.openapis.org/oas/v3.1.0#components-object
var reComponentKey = regexp.MustCompile(`^[a-zA-Z0-9.\-_]+$`)

// ErrSchemaNotFound is returned when the schema to rename is not in
// components.schemas.
type ErrSchemaNotFound struct{ Name string }

func (e *ErrSchemaNotFound) Error() string {
	return fmt.Sprintf("schema %q not found in components.schemas", e.Name)
}

// ErrSchemaExists is returned when the new name is already taken by another
// schema. Renaming onto it would silently merge two definitions into one.
type ErrSchemaExists struct{ Name string }

func (e *ErrSchemaExists) Error() string {
	return fmt.Sprintf("schema %q already exists in components.schemas", e.Name)
}

// ErrInvalidSchemaName is returned when the new name is not a valid key under
// components, and so could not be referenced.
type ErrInvalidSchemaName struct{ Name string }

func (e *ErrInvalidSchemaName) Error() string {
	return fmt.Sprintf("schema name %q must match %s", e.Name, reComponentKey)
}

// RenameSchema renames a schema in components.schemas and rewrites every
// reference to it, wherever in the document that reference occurs.
//
// The schema keeps its position among the components, so renaming produces a
// one-line change rather than reordering the section.
//
// Renaming a schema to its current name does nothing and reports no error.
// Otherwise it fails, changing nothing, if:
//
//   - oldName is not in components.schemas ([ErrSchemaNotFound]);
//   - newName is already taken ([ErrSchemaExists]) — renaming onto an existing
//     schema would silently discard one of two different definitions, and
//     point every reference to whichever survived;
//   - newName could not be referenced ([ErrInvalidSchemaName]).
func RenameSchema(doc *openapi.Document, oldName, newName string) error {
	schemas := doc.Components.Schemas

	s, ok := schemas[oldName]
	if !ok {
		return &ErrSchemaNotFound{Name: oldName}
	}

	if oldName == newName {
		return nil
	}

	if !reComponentKey.MatchString(newName) {
		return &ErrInvalidSchemaName{Name: newName}
	}

	if _, exists := schemas[newName]; exists {
		return &ErrSchemaExists{Name: newName}
	}

	// Assign directly rather than through Set: the schema carries its own
	// ordering index, so moving the value to a new key keeps it where it was,
	// while Set would move it to the end of the section.
	delete(schemas, oldName)
	schemas[newName] = s

	renameRefs(doc, schemaRefPrefix+oldName, schemaRefPrefix+newName)

	return nil
}

// renameRefs rewrites every schema reference in doc from old to new.
func renameRefs(doc *openapi.Document, old, new string) {
	walkSchemaRefs(doc, func(r *openapi.SchemaRef) {
		if r.Ref != nil && r.Ref.Identifier == old {
			r.Ref.Identifier = new
		}
	})
}
