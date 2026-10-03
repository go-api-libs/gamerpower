package ir

import "strings"

// MinimalLiteral is a Go literal of goType that marshals: every union in it that a value
// requires, itself included, has its first variant set.
func (doc Document) MinimalLiteral(goType string) string {
	lit, _ := doc.minimalLiteral(goType, map[string]bool{})
	return lit
}

// minimalLiteral also reports whether the literal sets anything.
func (doc Document) minimalLiteral(goType string, seen map[string]bool) (string, bool) {
	zero := goType + "{}"

	if seen[goType] {
		return zero, false
	}

	var s *Schema
	for j := range doc.Schemas {
		if doc.Schemas[j].Name == goType {
			s = &doc.Schemas[j]
			break
		}
	}

	if s == nil {
		return zero, false
	}

	seen[goType] = true
	defer delete(seen, goType)

	var parts []string

	switch s.Kind {
	case SchemaKindUnion:
		if len(s.UnionVariants) == 0 {
			return zero, false
		}

		v := s.UnionVariants[0]

		var lit string

		switch v.Zero {
		case "":
			l, set := doc.minimalLiteral(v.Type, seen)
			if set {
				lit = "&" + l
			} else {
				lit = "new(" + v.Type + ")"
			}
		case `""`:
			lit = `"-"` // any but the empty string, which reads as not set
		default:
			lit = v.Type + "{}" // empty, but not nil
			if v.Type == "any" {
				lit = "struct{}{}"
			}
		}

		parts = append(parts, v.FieldName+": "+lit)
	case SchemaKindStruct, SchemaKindAllOf:
		for _, f := range s.Fields {
			if strings.HasPrefix(f.Type, "*") || !f.Required && !f.Embedded {
				continue
			}

			if lit, set := doc.minimalLiteral(f.Type, seen); set {
				name := f.Name
				if f.Embedded {
					name = f.Type
				}

				parts = append(parts, name+": "+lit)
			}
		}
	default:
	}

	if len(parts) == 0 {
		return zero, false
	}

	return goType + "{" + strings.Join(parts, ", ") + "}", true
}
