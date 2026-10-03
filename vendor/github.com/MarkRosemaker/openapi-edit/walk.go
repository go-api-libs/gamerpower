package edit

import (
	"maps"
	"slices"

	"github.com/MarkRosemaker/openapi"
)

// walkSchemas calls fn once for every schema reachable from doc: through
// components (schemas, responses, parameters, request bodies, headers,
// callbacks, path items), through every path, operation, and webhook, and
// through the schemas each one contains or refers to.
//
// It visits them in the order the document holds them, so an edit that depends
// on which it meets first gives the same result every time.
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

	for _, s := range doc.Components.Schemas.ByIndex() {
		w.schema(s)
	}

	for _, r := range doc.Components.Responses.ByIndex() {
		w.response(r)
	}

	for _, p := range doc.Components.Parameters.ByIndex() {
		w.parameter(p)
	}

	for _, rb := range doc.Components.RequestBodies.ByIndex() {
		w.requestBody(rb)
	}

	w.headers(doc.Components.Headers)

	for _, c := range doc.Components.Callbacks.ByIndex() {
		w.callbackRef(c)
	}

	for _, p := range doc.Components.PathItems.ByIndex() {
		w.pathItemRef(p)
	}

	for _, p := range doc.Paths.ByIndex() {
		w.pathItem(p)
	}

	for _, p := range doc.Webhooks.ByIndex() {
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

	for _, r := range op.Responses.ByIndex() {
		w.response(r)
	}

	// an operation's callbacks keep no order of their own
	for _, name := range slices.Sorted(maps.Keys(op.Callbacks)) {
		w.callback(op.Callbacks[name])
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
	for _, p := range c.ByIndex() {
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
	for _, r := range hs.ByIndex() {
		if r == nil || r.Value == nil {
			continue
		}

		w.schema(r.Value.Schema)
		w.content(r.Value.Content)
	}
}

func (w *schemaWalker) content(c openapi.Content) {
	for _, mt := range c.ByIndex() {
		if mt == nil {
			continue
		}

		w.schema(mt.Schema)

		for _, e := range mt.Encoding.ByIndex() {
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

	subschemas(s, w.schema)
}

// subschemas calls fn for each schema s holds inline, in the order s holds them; a reference's target is not one.
func subschemas(s *openapi.Schema, fn func(*openapi.Schema)) {
	for _, l := range []openapi.SchemaList{s.AllOf, s.OneOf, s.AnyOf} {
		for _, e := range l {
			fn(e)
		}
	}

	fn(s.Not)

	for _, e := range s.PrefixItems {
		fn(e)
	}

	fn(s.Items)

	if s.AdditionalProperties != nil {
		fn(s.AdditionalProperties.Schema)
	}

	fn(s.PropertyNames)

	for _, p := range s.Properties.ByIndex() {
		fn(p)
	}
}
