package edit

import "github.com/MarkRosemaker/openapi"

// walkSchemaRefs calls fn once for every schema reference reachable from doc:
// through components (schemas, responses, parameters, request bodies,
// headers, callbacks, path items) and through every path, operation, and
// webhook.
//
// A schema reachable through more than one reference — directly, or because
// two references resolve to the same schema — is still walked into only
// once, so fn can freely mutate the references it's given without risking
// infinite recursion on a self-referential schema.
//
// This is the traversal RenameSchema uses to find every occurrence of a
// reference; it's exported because other structural edits need the same
// walk with a different fn, e.g. finding every reference to a schema that's
// about to be redirected onto another with [RedirectSchema].
func walkSchemaRefs(doc *openapi.Document, fn func(*openapi.SchemaRef)) {
	w := &schemaRefWalker{fn: fn, visited: map[*openapi.Schema]bool{}}

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

// schemaRefWalker walks every schema reference reachable from a document,
// calling fn once for each.
type schemaRefWalker struct {
	fn      func(*openapi.SchemaRef)
	visited map[*openapi.Schema]bool
}

func (w *schemaRefWalker) pathItemRef(r *openapi.PathItemRef) {
	if r != nil {
		w.pathItem(r.Value)
	}
}

func (w *schemaRefWalker) pathItem(p *openapi.PathItem) {
	if p == nil {
		return
	}

	w.parameterList(p.Parameters)

	for _, op := range p.Operations {
		w.operation(op)
	}
}

func (w *schemaRefWalker) operation(op *openapi.Operation) {
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
func (w *schemaRefWalker) callbackRef(r *openapi.CallbackRef) {
	if r != nil && r.Value != nil {
		w.callback(*r.Value)
	}
}

func (w *schemaRefWalker) callback(c openapi.Callback) {
	for _, p := range c {
		w.pathItemRef(p)
	}
}

func (w *schemaRefWalker) parameterList(ps openapi.ParameterList) {
	for _, p := range ps {
		w.parameter(p)
	}
}

func (w *schemaRefWalker) parameter(r *openapi.ParameterRef) {
	if r == nil || r.Value == nil {
		return
	}

	w.schemaRef(r.Value.Schema)
	w.content(r.Value.Content)
}

func (w *schemaRefWalker) requestBody(r *openapi.RequestBodyRef) {
	if r == nil || r.Value == nil {
		return
	}

	w.content(r.Value.Content)
}

func (w *schemaRefWalker) response(r *openapi.ResponseRef) {
	if r == nil || r.Value == nil {
		return
	}

	w.headers(r.Value.Headers)
	w.content(r.Value.Content)
}

func (w *schemaRefWalker) headers(hs openapi.Headers) {
	for _, r := range hs {
		if r == nil || r.Value == nil {
			continue
		}

		w.schema(r.Value.Schema)
		w.content(r.Value.Content)
	}
}

func (w *schemaRefWalker) content(c openapi.Content) {
	for _, mt := range c {
		if mt == nil {
			continue
		}

		w.schemaRef(mt.Schema)

		for _, e := range mt.Encoding {
			if e != nil {
				w.headers(e.Headers)
			}
		}
	}
}

func (w *schemaRefWalker) schemaRef(r *openapi.SchemaRef) {
	if r == nil {
		return
	}

	w.fn(r)

	// A resolved reference also carries the schema it points at. Walking it is
	// what reaches references nested inside a referenced schema.
	w.schema(r.Value)
}

func (w *schemaRefWalker) schemaRefList(l openapi.SchemaRefList) {
	for _, r := range l {
		w.schemaRef(r)
	}
}

func (w *schemaRefWalker) schema(s *openapi.Schema) {
	if s == nil || w.visited[s] {
		return
	}

	w.visited[s] = true

	w.schemaRefList(s.AllOf)
	w.schemaRefList(s.OneOf)
	w.schemaRefList(s.AnyOf)
	w.schemaRef(s.Not)
	w.schemaRef(s.Items)
	w.schemaRef(s.AdditionalProperties)

	for _, r := range s.Properties {
		w.schemaRef(r)
	}
}
