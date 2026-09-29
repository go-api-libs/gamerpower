package edit

import "github.com/MarkRosemaker/openapi"

// walkSchemas calls fn once for every schema reachable from doc: through
// components (schemas, responses, parameters, request bodies, headers,
// callbacks, path items), through every path, operation, and webhook, and
// through the schemas each one contains or refers to.
//
// A schema with a $ref is itself one of them, so fn sees every reference, and
// the keywords beside it too. Each schema is walked into only once, so fn can
// freely edit what it is given without risking infinite recursion on a
// self-referential schema.
//
// This is the traversal RenameSchema uses to find every occurrence of a
// reference; it's shared because other structural edits need the same
// walk with a different fn, e.g. finding every reference to a schema that's
// about to be redirected onto another with [RedirectSchema].
func walkSchemas(doc *openapi.Document, fn func(*openapi.Schema)) {
	w := &schemaWalker{fn: fn, visited: map[*openapi.Schema]bool{}}

	for _, s := range doc.Components.Schemas {
		w.schema(s)
	}

	for _, r := range doc.Components.Responses {
		w.response(r)
	}

	for _, p := range doc.Components.Parameters {
		w.parameter(p)
	}

	for _, rb := range doc.Components.RequestBodies {
		w.requestBody(rb)
	}

	w.headers(doc.Components.Headers)

	for _, c := range doc.Components.Callbacks {
		w.callbackRef(c)
	}

	for _, p := range doc.Components.PathItems {
		w.pathItemRef(p)
	}

	for _, p := range doc.Paths {
		w.pathItem(p)
	}

	for _, p := range doc.Webhooks {
		w.pathItemRef(p)
	}
}

// schemaWalker walks every schema reference reachable from a document,
// calling fn once for each.
type schemaWalker struct {
	fn      func(*openapi.Schema)
	visited map[*openapi.Schema]bool
}

func (w *schemaWalker) pathItemRef(r *openapi.PathItemRef) {
	if r != nil {
		w.pathItem(r.Value)
	}
}

func (w *schemaWalker) pathItem(p *openapi.PathItem) {
	if p == nil {
		return
	}

	w.parameterList(p.Parameters)

	for _, op := range p.Operations {
		w.operation(op)
	}
}

func (w *schemaWalker) operation(op *openapi.Operation) {
	if op == nil {
		return
	}

	w.parameterList(op.Parameters)
	w.requestBody(op.RequestBody)

	for _, r := range op.Responses {
		w.response(r)
	}

	for _, c := range op.Callbacks {
		w.callback(c)
	}
}

// callbackRef covers components.callbacks, which holds references, whereas an
// operation holds callbacks by value.
func (w *schemaWalker) callbackRef(r *openapi.CallbackRef) {
	if r != nil && r.Value != nil {
		w.callback(*r.Value)
	}
}

func (w *schemaWalker) callback(c openapi.Callback) {
	for _, p := range c {
		w.pathItemRef(p)
	}
}

func (w *schemaWalker) parameterList(ps openapi.ParameterList) {
	for _, p := range ps {
		w.parameter(p)
	}
}

func (w *schemaWalker) parameter(r *openapi.ParameterRef) {
	if r == nil || r.Value == nil {
		return
	}

	w.schema(r.Value.Schema)
	w.content(r.Value.Content)
}

func (w *schemaWalker) requestBody(r *openapi.RequestBodyRef) {
	if r == nil || r.Value == nil {
		return
	}

	w.content(r.Value.Content)
}

func (w *schemaWalker) response(r *openapi.ResponseRef) {
	if r == nil || r.Value == nil {
		return
	}

	w.headers(r.Value.Headers)
	w.content(r.Value.Content)
}

func (w *schemaWalker) headers(hs openapi.Headers) {
	for _, r := range hs {
		if r == nil || r.Value == nil {
			continue
		}

		w.schema(r.Value.Schema)
		w.content(r.Value.Content)
	}
}

func (w *schemaWalker) content(c openapi.Content) {
	for _, mt := range c {
		if mt == nil {
			continue
		}

		w.schema(mt.Schema)

		for _, e := range mt.Encoding {
			if e != nil {
				w.headers(e.Headers)
			}
		}
	}
}

func (w *schemaWalker) schemaList(l openapi.SchemaList) {
	for _, s := range l {
		w.schema(s)
	}
}

func (w *schemaWalker) schema(s *openapi.Schema) {
	if s == nil || w.visited[s] {
		return
	}

	w.visited[s] = true

	w.fn(s)

	// A resolved reference also carries the schema it points at. Walking it is
	// what reaches references nested inside a referenced schema.
	if s.Ref != nil {
		w.schema(s.Ref.Value)
	}

	w.schemaList(s.AllOf)
	w.schemaList(s.OneOf)
	w.schemaList(s.AnyOf)
	w.schema(s.Not)
	w.schemaList(s.PrefixItems)
	w.schema(s.Items)

	if s.AdditionalProperties != nil {
		w.schema(s.AdditionalProperties.Schema)
	}

	for _, p := range s.Properties {
		w.schema(p)
	}
}
