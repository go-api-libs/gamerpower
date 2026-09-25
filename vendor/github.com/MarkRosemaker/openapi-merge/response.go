package merge

import (
	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

func Response(a, b *openapi.Response) error {
	a.Description = mergeString(a.Description, b.Description)

	// if err := r.Headers.Validate(); err != nil {
	// 	return &errpath.ErrField{Field: "headers", Err: err}
	// }

	if err := Content(&a.Content, b.Content); err != nil {
		return &errpath.ErrField{Field: "content", Err: err}
	}

	// if err := r.Links.Validate(); err != nil {
	// 	return &errpath.ErrField{Field: "links", Err: err}
	// }

	return extensions(a.Extensions, b.Extensions)
}

// func (l *loader) collectResponseRef(r *ResponseRef, ref ref) {
// 	if r.Value != nil {
// 		l.collectResponse(r.Value, ref)
// 	}
// }

// func (l *loader) collectResponse(r *Response, ref ref) {
// 	l.responses[ref.String()] = r
// }

// func (l *loader) resolveResponseRef(r *ResponseRef) error {
// 	return resolveRef(r, l.responses, l.resolveResponse)
// }

// func (l *loader) resolveResponse(r *Response) error {
// 	if err := l.resolveHeaders(r.Headers); err != nil {
// 		return &errpath.ErrField{Field: "headers", Err: err}
// 	}

// 	if err := l.resolveContent(r.Content); err != nil {
// 		return &errpath.ErrField{Field: "content", Err: err}
// 	}

// 	if err := l.resolveLinks(r.Links); err != nil {
// 		return &errpath.ErrField{Field: "links", Err: err}
// 	}

// 	return nil
// }
