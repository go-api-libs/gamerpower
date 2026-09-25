package flatten

import (
	"fmt"

	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

type mode int

const (
	moveIfNecessary mode = iota
	alwaysMove
	neverMove
)

func schemaRef(d *openapi.Document, s *openapi.SchemaRef, name string, mode mode) error {
	if s.Ref != nil {
		return nil // already processed
	}

	if mode == alwaysMove {
		moveSchemaToComponents(d, name, s)

		// process the schema itself
		return schema(d, s.Value, name)
	}

	switch s.Value.Type {
	case openapi.TypeInteger, openapi.TypeNumber, openapi.TypeBoolean, openapi.TypeNull: // no need to move to components
	case openapi.TypeString:
		if s.Value.Enum != nil && mode != neverMove {
			moveSchemaToComponents(d, name, s)
		} // else just string, no need to move to components
	case openapi.TypeArray:
		items := s.Value.Items.Value
		switch items.Type {
		case openapi.TypeInteger: // do nothing, just []int
		case openapi.TypeNumber: // do nothing, just []float32 or []float64
		case openapi.TypeString:
			if items.Enum != nil && mode != neverMove {
				moveSchemaToComponents(d, name, s)
			} // else just []string, no need to move to components
		case openapi.TypeObject:
			if len(items.Properties) > 0 && mode != neverMove {
				moveSchemaToComponents(d, name, s)
			}
		case openapi.TypeBoolean: // do nothing, just []bool
		case openapi.TypeNull: // do nothing, just []null
		case openapi.TypeArray: // TODO: later
		case "": // no explicit type — items uses anyOf / oneOf / allOf (e.g. nullable union)
		default:
			return fmt.Errorf("unimplemented item type %q", items.Type)
		}
	case openapi.TypeObject: // move to components
		if len(s.Value.Properties) > 0 && mode != neverMove {
			moveSchemaToComponents(d, name, s)
		}
	case "": // no explicit type — oneOf / anyOf / allOf composition or bare properties
		v := s.Value
		hasComposition := len(v.OneOf) > 0 || len(v.AnyOf) > 0 || len(v.AllOf) > 0
		if (hasComposition || len(v.Properties) > 0) && mode != neverMove {
			moveSchemaToComponents(d, name, s)
		}
	default:
		return fmt.Errorf("unimplemented schema ref type %q", s.Value.Type)
	}

	// process the schema itself
	return schema(d, s.Value, name)
}

func schema(d *openapi.Document, s *openapi.Schema, name string) error {
	switch s.Type {
	case openapi.TypeString,
		openapi.TypeInteger,
		openapi.TypeNumber,
		openapi.TypeBoolean,
		openapi.TypeNull: // no need to do anything
		return nil
	case openapi.TypeArray, openapi.TypeObject: // do below
	case "": // is valid if schema contains oneOf, anyOf, allOf, or properties
	default:
		return fmt.Errorf("unimplemented schema type %q", s.Type)
	}

	if err := schemaRefList(d, s.AllOf, name+"AllOf"); err != nil {
		return &errpath.ErrField{Field: "allOf", Err: err}
	}

	if err := schemaRefList(d, s.OneOf, name+"OneOf"); err != nil {
		return &errpath.ErrField{Field: "oneOf", Err: err}
	}

	if err := schemaRefList(d, s.AnyOf, name+"AnyOf"); err != nil {
		return &errpath.ErrField{Field: "anyOf", Err: err}
	}

	if s.Items != nil {
		if err := schemaRef(d, s.Items, name+"Item", moveIfNecessary); err != nil {
			return &errpath.ErrField{Field: "items", Err: err}
		}
	}

	if err := schemaRefs(d, s.Properties, name); err != nil {
		return &errpath.ErrField{Field: "properties", Err: err}
	}

	if s.AdditionalProperties != nil {
		if err := schemaRef(d, s.AdditionalProperties, name+"Value", moveIfNecessary); err != nil {
			return &errpath.ErrField{Field: "additionalProperties", Err: err}
		}
	}

	return nil
}

func moveSchemaToComponents(d *openapi.Document, name string, s *openapi.SchemaRef) {
	// reference the schema in the components
	name = uniqueName(d.Components.Schemas, name)
	d.Components.Schemas.Set(name, s.Value)
	s.Ref = newRef("schemas", name)
}

func schemaRefList(d *openapi.Document, ss openapi.SchemaRefList, prefix string) error {
	for i, s := range ss {
		if err := schemaRef(d, s, fmt.Sprintf("%s%d", prefix, i), neverMove); err != nil {
			return &errpath.ErrIndex{Index: i, Err: err}
		}
	}

	return nil
}
