package merge

import (
	"fmt"

	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

func Parameter(a, b *openapi.Parameter) error {
	a.Name = mergeString(a.Name, b.Name)

	if a.In != b.In {
		return &errpath.ErrField{
			Field: "in",
			Err:   fmt.Errorf("%q vs. %q", a.In, b.In),
		}
	}

	// NOTE: a.Required, a.AllowEmptyValue, a.AllowReserved stay as is

	a.Description = mergeString(a.Description, b.Description)

	// A parameter MUST contain either a `schema` property, or a `content` property, but not both.
	if b.Schema != nil {
		if a.Schema != nil {
			if err := Schema(a.Schema.Value, b.Schema.Value, true); err != nil {
				return &errpath.ErrField{Field: "schema", Err: err}
			}

			if a.Schema.Value.Type == openapi.TypeArray {
				// TODO: only delete if example is no longer an array
				a.Example = nil
				b.Example = nil
			}
		} else {
			return fmt.Errorf("merging content with schema unimplemented")
		}
	} else {
		// 	if len(p.Content) != 1 {
		// 		return &errpath.ErrField{Field: "content", Err: &errpath.ErrInvalid[string]{
		// 			Message: fmt.Sprintf("must contain exactly one entry, got %d", len(p.Content)),
		// 		}}
		// 	}

		// 	if err := p.Content.Validate(); err != nil {
		// 		return &errpath.ErrField{Field: "content", Err: err}
		// 	}
		return fmt.Errorf("merging content unimplemented")
	}

	// if p.Style != "" {
	// 	if err := p.Style.Validate(); err != nil {
	// 		return &errpath.ErrField{Field: "style", Err: err}
	// 	}
	// }

	if a.Explode != nil || b.Explode != nil {
		if a.Explode == nil {
			a.Explode = b.Explode
		} else if b.Explode == nil {
			b.Explode = a.Explode
		} else if *a.Explode != *b.Explode {
			// true is default but one is not that, so set both to non-default
			no := false
			a.Explode = &no
			b.Explode = &no
		}
	}

	if a.Example == nil && a.Examples == nil {
		a.Example = b.Example
	}

	// if err := p.Examples.Validate(); err != nil {
	// 	return &errpath.ErrField{Field: "examples", Err: err}
	// }

	return extensions(a.Extensions, b.Extensions)
}

// func (l *loader) collectParameterRef(p *ParameterRef, ref ref) {
// 	if p.Value != nil {
// 		l.collectParameter(p.Value, ref)
// 	}
// }

// func (l *loader) collectParameter(p *Parameter, ref ref) {
// 	l.parameters[ref.String()] = p
// }

// func (l *loader) resolveParameterRef(p *ParameterRef) error {
// 	return resolveRef(p, l.parameters, l.resolveParameter)
// }

// func (l *loader) resolveParameter(p *Parameter) error {
// 	if p.Schema != nil {
// 		if err := l.resolveSchema(p.Schema); err != nil {
// 			return &errpath.ErrField{Field: "schema", Err: err}
// 		}
// 	}

// 	if err := l.resolveContent(p.Content); err != nil {
// 		return &errpath.ErrField{Field: "content", Err: err}
// 	}

// 	if err := l.resolveExamples(p.Examples); err != nil {
// 		return &errpath.ErrField{Field: "examples", Err: err}
// 	}

// 	return nil
// }
