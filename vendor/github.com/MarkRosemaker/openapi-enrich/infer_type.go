package enrich

import (
	"encoding/json/jsontext"
	"math"
	"strconv"

	"github.com/MarkRosemaker/openapi"
)

// inferTypes gives every schema in doc without a type the one its enum and const values imply.
func inferTypes(doc *openapi.Document) {
	walkSchemas(doc, inferType)
}

// walkSchemas calls fn once for every schema reachable from doc.
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
		if c != nil && c.Value != nil {
			w.callback(*c.Value)
		}
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

	for _, r := range p.Parameters {
		w.parameter(r)
	}

	for _, op := range p.Operations {
		for _, r := range op.Parameters {
			w.parameter(r)
		}

		w.requestBody(op.RequestBody)

		for _, r := range op.Responses {
			w.response(r)
		}

		for _, c := range op.Callbacks {
			w.callback(c)
		}
	}
}

func (w *schemaWalker) callback(c openapi.Callback) {
	for _, p := range c {
		w.pathItemRef(p)
	}
}

func (w *schemaWalker) parameter(r *openapi.ParameterRef) {
	if r != nil && r.Value != nil {
		w.schema(r.Value.Schema)
		w.content(r.Value.Content)
	}
}

func (w *schemaWalker) requestBody(r *openapi.RequestBodyRef) {
	if r != nil && r.Value != nil {
		w.content(r.Value.Content)
	}
}

func (w *schemaWalker) response(r *openapi.ResponseRef) {
	if r != nil && r.Value != nil {
		w.headers(r.Value.Headers)
		w.content(r.Value.Content)
	}
}

func (w *schemaWalker) headers(hs openapi.Headers) {
	for _, r := range hs {
		if r != nil && r.Value != nil {
			w.schema(r.Value.Schema)
			w.content(r.Value.Content)
		}
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

	w.schema(s.PropertyNames)

	for _, r := range s.Properties {
		w.schema(r)
	}
}

// inferType sets the type of s from its enum and const values, if it has none and they are of a single kind.
func inferType(s *openapi.Schema) {
	if s.Type != "" {
		return
	}

	values := s.Enum
	if len(s.Const) > 0 {
		values = append(values[:len(values):len(values)], s.Const)
	}

	var (
		typ      openapi.DataType
		nullable bool
	)

	for _, v := range values {
		t := typeOf(v)
		switch {
		case t == openapi.TypeNull:
			nullable = true
		case typ == "" || typ == t:
			typ = t
		case typ == openapi.TypeInteger && t == openapi.TypeNumber,
			typ == openapi.TypeNumber && t == openapi.TypeInteger:
			typ = openapi.TypeNumber
		default:
			return // values of several kinds
		}
	}

	switch {
	case typ != "":
		s.Type, s.Nullable = typ, nullable
	case nullable:
		s.Type = openapi.TypeNull
	}
}

// typeOf returns the JSON Schema type of v, which is integer for any number without a fractional part.
func typeOf(v jsontext.Value) openapi.DataType {
	switch v.Kind() {
	case '"':
		return openapi.TypeString
	case 't', 'f':
		return openapi.TypeBoolean
	case 'n':
		return openapi.TypeNull
	case '[':
		return openapi.TypeArray
	case '{':
		return openapi.TypeObject
	case '0':
		if f, err := strconv.ParseFloat(string(v), 64); err == nil && f == math.Trunc(f) {
			return openapi.TypeInteger
		}

		return openapi.TypeNumber
	default:
		return ""
	}
}
