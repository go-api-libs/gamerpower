// Package schema provides equality and similarity comparisons for
// github.com/MarkRosemaker/openapi Schema objects.
package schema

import (
	"bytes"
	"encoding/json/jsontext"
	"regexp"
	"slices"

	"github.com/MarkRosemaker/openapi"
)

// Equal reports whether a and b are fully identical, including
// documentation fields (Title, Description, Default, Extensions).
//
// Example is always ignored: per the OpenAPI/JSON Schema spec it is
// documentation only and never affects what an instance validates against.
func Equal(a, b *openapi.Schema) bool {
	if a == b {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	return a.Title == b.Title &&
		a.Description == b.Description &&
		bytes.Equal(a.Default, b.Default) &&
		sameCore(a, b, schemaRefEqual)
}

// SameShape reports whether a and b validate identically: the same JSON
// instances would pass or fail against both schemas.
//
// It ignores documentation-only fields: Title, Description, Default, and
// Example. Extensions are still compared, since custom "x-" extensions can
// carry semantic meaning that a generic comparison can't rule out.
func SameShape(a, b *openapi.Schema) bool {
	if a == b {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	return sameCore(a, b, schemaRefSameShape)
}

// sameCore compares the fields that determine what an instance validates
// against, recursing into nested schemas through refMatch. refMatch decides
// how strictly nested schemas are compared: schemaRefEqual for a full Equal
// walk, schemaRefSameShape for a shape-only SameShape walk.
func sameCore(a, b *openapi.Schema, refMatch func(a, b *openapi.SchemaRef) bool) bool {
	return a.Type == b.Type &&
		a.Format == b.Format &&
		schemaRefListsMatch(a.AllOf, b.AllOf, refMatch) &&
		schemaRefListsMatch(a.OneOf, b.OneOf, refMatch) &&
		schemaRefListsMatch(a.AnyOf, b.AnyOf, refMatch) &&
		refMatch(a.Not, b.Not) &&
		ptrsEqual(a.Min, b.Min) &&
		ptrsEqual(a.Max, b.Max) &&
		regexpsEqual(a.Pattern, b.Pattern) &&
		slices.EqualFunc(a.Enum, b.Enum,
			func(c, d jsontext.Value) bool { return bytes.Equal(c, d) }) &&
		a.MinItems == b.MinItems &&
		ptrsEqual(a.MaxItems, b.MaxItems) &&
		refMatch(a.Items, b.Items) &&
		schemaRefsMatch(a.Properties, b.Properties, refMatch) &&
		slices.Equal(a.Required, b.Required) &&
		refMatch(a.AdditionalProperties, b.AdditionalProperties) &&
		a.ContentMediaType == b.ContentMediaType &&
		a.ContentEncoding == b.ContentEncoding &&
		bytes.Equal(a.Extensions, b.Extensions)
}

// schemaRefEqual reports whether a and b are fully identical, recursing
// through Equal.
func schemaRefEqual(a, b *openapi.SchemaRef) bool {
	if a == b {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	switch {
	case a.Ref != nil && b.Ref != nil:
		return a.Ref.Identifier == b.Ref.Identifier &&
			a.Ref.Summary == b.Ref.Summary &&
			a.Ref.Description == b.Ref.Description
	case a.Ref == nil && b.Ref == nil:
		return Equal(a.Value, b.Value)
	default:
		return false
	}
}

// schemaRefSameShape reports whether a and b validate identically,
// recursing through SameShape. Unlike schemaRefEqual, it ignores a
// reference's Summary/Description overrides, since those are documentation
// only.
func schemaRefSameShape(a, b *openapi.SchemaRef) bool {
	if a == b {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	switch {
	case a.Ref != nil && b.Ref != nil:
		return a.Ref.Identifier == b.Ref.Identifier
	case a.Ref == nil && b.Ref == nil:
		return SameShape(a.Value, b.Value)
	default:
		return false
	}
}

func schemaRefListsMatch(a, b openapi.SchemaRefList, match func(a, b *openapi.SchemaRef) bool) bool {
	return slices.EqualFunc(a, b, match)
}

func schemaRefsMatch(a, b openapi.SchemaRefs, match func(a, b *openapi.SchemaRef) bool) bool {
	if len(a) != len(b) {
		return false
	}

	for k, va := range a {
		vb, ok := b[k]
		if !ok || !match(va, vb) {
			return false
		}
	}

	return true
}

func ptrsEqual[T comparable](a, b *T) bool {
	if a == b {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	return *a == *b
}

func regexpsEqual(a, b *regexp.Regexp) bool {
	if a == b {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	return a.String() == b.String()
}
