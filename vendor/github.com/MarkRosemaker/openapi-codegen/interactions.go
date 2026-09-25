package codegen

import (
	"encoding/json/v2"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/MarkRosemaker/openapi-codegen/ir"
	"github.com/MarkRosemaker/openapi-enrich/cassette"
)

// matchInteractions populates doc.InteractionCalls by matching each interaction
// to a known operation and extracting Go literal argument values.
// Every interaction must match exactly one operation; an error is returned otherwise,
// guaranteeing len(doc.InteractionCalls) == len(interactions) on success.
func matchInteractions(doc *ir.Document, interactions cassette.Interactions) error {
	fillGlobalParamExamples(doc, interactions)

	for _, ia := range interactions {
		if ia.Request.URL == "" {
			continue // just a scaffold
		}

		u, err := url.Parse(ia.Request.URL)
		if err != nil {
			return err
		}

		// Strip the base URL path prefix to get the operation-relative path.
		relPath := strings.TrimPrefix(u.Path, doc.BaseURL.Path)
		if relPath == "" {
			relPath = "/"
		} else if !strings.HasPrefix(relPath, "/") {
			relPath = "/" + relPath
		}

		op, pathVals := pickOperation(doc.Operations, ia.Request.Method, relPath)
		if op == nil {
			return fmt.Errorf("interaction %s %s: no matching operation found", ia.Request.Method, ia.Request.URL)
		}

		call := ir.InteractionCall{Op: op}

		for _, pp := range op.PathParams {
			call.PathArgs = append(call.PathArgs, goLiteralForType(pp.Type, pathVals[pp.JSONName]))
		}

		q := u.Query()
		for _, qp := range op.QueryParams {
			val := q.Get(qp.JSONName)
			if val != "" {
				call.QueryArgs = append(call.QueryArgs, ir.InteractionParam{
					FieldName: qp.FieldName,
					Literal:   goLiteralForType(qp.Type, val),
				})
			}
		}

		for _, hp := range op.HeaderParams {
			val := ia.Request.Headers.Get(hp.JSONName)
			if val != "" {
				call.HeaderArgs = append(call.HeaderArgs, ir.InteractionParam{
					FieldName: hp.FieldName,
					Literal:   goLiteralForType(hp.Type, val),
				})
			}
		}

		if op.RequestBody != nil {
			goType := op.RequestBody.TypeName
			if !op.RequestBody.Required {
				goType = "*" + goType
			}

			var raw any
			if len(ia.Request.Body) > 0 {
				if err := json.Unmarshal(ia.Request.Body, &raw); err != nil {
					return fmt.Errorf("decoding request body for %s: %w", op.Name, err)
				}
			}

			call.BodyLiteral = bodyLiteral(doc, goType, raw)
		}

		if r := findResponse(op, ia.Response.StatusCode); r != nil {
			call.IsSuccess = r.IsSuccess
			switch {
			case r.IsSuccess:
			case r.IsRawBytes:
				// Nothing was decoded, so there is no generated type to match
				// on: the client hands the body back as an api.ErrorBody.
				call.ErrorType = "api.ErrorBody"
			case r.GoType != nil:
				call.ErrorType = r.GoType.String()
			}
		} else {
			// No declared response for this exact status code: fall back to the
			// HTTP convention so the generated assertion at least checks err==nil
			// vs err!=nil correctly, without asserting a specific error type.
			call.IsSuccess = ia.Response.StatusCode >= 200 && ia.Response.StatusCode < 300
		}

		doc.InteractionCalls = append(doc.InteractionCalls, call)
	}

	return nil
}

// fillGlobalParamExamples gives every global parameter the specification has no
// example for the value that was recorded for it, so that the client default and
// the test environment agree with the cassette they are replayed against.
//
// The specification is not a reliable source here: an example is only present
// when the recorded value survived masking, so credentials tend to be exactly
// the parameters missing one.
func fillGlobalParamExamples(doc *ir.Document, interactions cassette.Interactions) {
	for i := range doc.GlobalParams {
		p := &doc.GlobalParams[i]
		if p.Example != "" {
			continue
		}

		for _, ia := range interactions {
			if val := ia.Request.Headers.Get(p.JSONName); val != "" {
				p.Example = strconv.Quote(val)
				break
			}
		}
	}
}

// findResponse returns the operation's declared response matching statusCode,
// or nil if the operation has no response entry for that exact numeric code.
func findResponse(op *ir.Operation, statusCode int) *ir.Response {
	for i := range op.Responses {
		r := &op.Responses[i]
		if n, err := strconv.Atoi(r.StatusCode); err == nil && n == statusCode {
			return r
		}
	}

	return nil
}

// pickOperation returns the operation whose method and path template match the
// URL, preferring an exact segment-count match over a trailing-wildcard match
// so that e.g. /tasks/{taskId}/score/up wins over /tasks/{taskId} for a URL
// like /tasks/abc/score/up.
func pickOperation(ops []ir.Operation, method, relPath string) (*ir.Operation, map[string]string) {
	var (
		wildcardOp   *ir.Operation
		wildcardVals map[string]string
	)
	for i := range ops {
		op := &ops[i]
		if op.Method != method {
			continue
		}

		vals, ok, exact := matchPathTemplate(op.PathTemplate, relPath)
		if !ok {
			continue
		}

		if exact {
			return op, vals
		}

		if wildcardOp == nil {
			wildcardOp = op
			wildcardVals = vals
		}
	}

	return wildcardOp, wildcardVals
}

// matchPathTemplate matches a URL path against an operation path template,
// returning the extracted parameter values keyed by parameter name.
// Template segments may be full-segment ({name}) or mid-segment (CIK{name}.json).
// A trailing pure {param} segment acts as a wildcard that consumes all remaining
// path segments joined by "/", enabling multi-segment captures like {path} in
// /package/{path} matching /package/github.com/user/repo. The third return
// value reports whether the match was segment-for-segment exact; callers should
// prefer an exact match over a wildcard one.
func matchPathTemplate(template, path string) (params map[string]string, matched, exact bool) {
	tParts := strings.Split(template, "/")
	pParts := strings.Split(path, "/")

	if len(tParts) > len(pParts) {
		return nil, false, false
	}

	params = make(map[string]string)
	for i, tp := range tParts {
		// Last template segment that is a pure {param} and there are extra path
		// segments: consume all remaining segments as one slash-joined value.
		if i == len(tParts)-1 &&
			strings.HasPrefix(tp, "{") &&
			strings.HasSuffix(tp, "}") &&
			strings.Count(tp, "{") == 1 &&
			len(pParts) > i+1 {
			params[tp[1:len(tp)-1]] = strings.Join(pParts[i:], "/")
			return params, true, false
		}

		if !extractSegmentParam(tp, pParts[i], params) {
			return nil, false, false
		}
	}

	sameLen := len(tParts) == len(pParts)

	return params, sameLen, sameLen
}

// extractSegmentParam matches one template segment against one path segment,
// populating out with any extracted parameter values. Returns false on mismatch.
func extractSegmentParam(tmpl, value string, out map[string]string) bool {
	if !strings.Contains(tmpl, "{") {
		return tmpl == value
	}

	// Simple case: entire segment is {name}
	if strings.HasPrefix(tmpl, "{") && strings.HasSuffix(tmpl, "}") && strings.Count(tmpl, "{") == 1 {
		out[tmpl[1:len(tmpl)-1]] = value
		return true
	}

	// Mid-segment case: e.g. "CIK{name}.json"
	start := strings.Index(tmpl, "{")
	end := strings.Index(tmpl, "}")
	if start < 0 || end <= start {
		return tmpl == value
	}

	prefix := tmpl[:start]
	suffix := tmpl[end+1:]
	name := tmpl[start+1 : end]

	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return false
	}

	extracted := value[len(prefix):]
	if len(suffix) > 0 {
		extracted = extracted[:len(extracted)-len(suffix)]
	}

	out[name] = extracted

	return true
}

// goLiteralForType returns a Go literal expression for value given the Go type.
func goLiteralForType(goType, value string) string {
	switch goType {
	case "int", "int32", "int64", "uint", "uint32", "uint64", "bool":
		return value
	case "uuid.UUID":
		return fmt.Sprintf("uuid.MustParse(%q)", value)
	default:
		return fmt.Sprintf("%q", value)
	}
}

// bodyLiteral returns a Go expression of type goType for the JSON value v (as
// decoded by json.Unmarshal into `any`: map[string]any, []any, string,
// float64, bool, or nil).
//
// It reproduces v as a composite literal wherever it recognizes the shape --
// a struct, an enum member, a slice, a pointer, or a plain scalar -- so the
// generated test reads like a value a person wrote, not a blob to decode.
// Anything it doesn't recognize (maps, unions, oneOf/anyOf, or a mismatch
// between v and what goType expects) falls back to decoding v's own compact
// JSON into goType at test time via mustDecodeBody, which is always correct
// even where a literal is not worth hand-rolling.
func bodyLiteral(doc *ir.Document, goType string, v any) string {
	switch {
	case v == nil:
		if strings.HasPrefix(goType, "*") {
			return "nil"
		}
		// A required value with nothing recorded for it: fall through to the
		// zero-value composite literal below rather than emit invalid Go.

	case strings.HasPrefix(goType, "*"):
		return fmt.Sprintf("new(%s)", bodyLiteral(doc, goType[1:], v))

	case strings.HasPrefix(goType, "[]"):
		arr, ok := v.([]any)
		if !ok {
			break
		}

		elems := make([]string, len(arr))
		for i, e := range arr {
			elems[i] = bodyLiteral(doc, goType[2:], e)
		}

		return fmt.Sprintf("%s{%s}", goType, strings.Join(elems, ", "))
	}

	if s := findSchema(doc, goType); s != nil {
		switch s.Kind {
		case ir.SchemaKindStruct, ir.SchemaKindAllOf:
			obj, isObj := v.(map[string]any)
			hasEmbedded := slices.ContainsFunc(s.Fields, func(f ir.Field) bool { return f.Embedded })

			// A non-object value where an object was expected, or a schema
			// with an allOf-embedded field (Field.Name is empty there, so it
			// has no JSON key of its own to look values up by): not worth
			// hand-rolling, fall through to the fallback below instead.
			if isObj && !hasEmbedded {
				var fields []string
				for _, f := range s.Fields {
					val, ok := obj[f.JSONName]
					if !ok {
						continue // absent optional field: leave it at its zero value
					}

					fields = append(fields, fmt.Sprintf("%s: %s", f.Name, bodyLiteral(doc, f.Type, val)))
				}

				if len(fields) == 0 {
					return goType + "{}"
				}

				return fmt.Sprintf("%s{\n%s,\n}", goType, strings.Join(fields, ",\n"))
			}

		case ir.SchemaKindEnum:
			if lit, ok := enumLiteral(s, v); ok {
				return lit
			}
		default:
		}

		// SchemaKindAlias, SchemaKindMap, SchemaKindUnion, or an enum value
		// that didn't match any declared member: not worth hand-rolling.
	}

	if lit, ok := scalarLiteral(goType, v); ok {
		return lit
	}

	return fmt.Sprintf("mustDecodeBody[%s](t, %s)", goType, fallbackJSON(v))
}

// findSchema returns doc's named component schema for goType, or nil if
// goType isn't the bare name of one (a built-in type, or a pointer/slice
// expression already stripped by the caller).
func findSchema(doc *ir.Document, goType string) *ir.Schema {
	for i := range doc.Schemas {
		if doc.Schemas[i].Name == goType {
			return &doc.Schemas[i]
		}
	}

	return nil
}

// enumLiteral returns the Go constant for the enum member of s matching v,
// formatted the same way ir.formatEnumValue derived each member's display
// form from the spec, so the comparison lines up exactly.
func enumLiteral(s *ir.Schema, v any) (string, bool) {
	var display string
	switch s.Type {
	case "string":
		str, ok := v.(string)
		if !ok {
			return "", false
		}

		display = str
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return "", false
		}

		display = strconv.FormatBool(b)
	default: // int-family or float-family
		f, ok := v.(float64)
		if !ok {
			return "", false
		}

		if strings.HasPrefix(s.Type, "float") {
			display = strconv.FormatFloat(f, 'g', -1, 64)
		} else {
			display = strconv.FormatInt(int64(f), 10)
		}
	}

	for _, ev := range s.EnumValues {
		if ev.Value == display {
			return ev.GoName, true
		}
	}

	return "", false
}

// scalarLiteral returns a Go literal for v as a built-in or well-known
// wrapper type, or ok=false when goType isn't one it knows how to express
// directly.
func scalarLiteral(goType string, v any) (string, bool) {
	switch goType {
	case "string":
		s, ok := v.(string)
		if !ok {
			return "", false
		}

		return fmt.Sprintf("%q", s), true

	case "bool":
		b, ok := v.(bool)
		if !ok {
			return "", false
		}

		return strconv.FormatBool(b), true

	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		f, ok := v.(float64)
		if !ok {
			return "", false
		}
		// An untyped integer constant assigns directly to any of these
		// field types, so no conversion is needed regardless of goType.
		return strconv.FormatInt(int64(f), 10), true

	case "float32", "float64":
		f, ok := v.(float64)
		if !ok {
			return "", false
		}

		return strconv.FormatFloat(f, 'g', -1, 64), true

	case "uuid.UUID":
		s, ok := v.(string)
		if !ok {
			return "", false
		}

		return fmt.Sprintf("uuid.MustParse(%q)", s), true

	default:
		return "", false
	}
}

// fallbackJSON returns v's compact JSON encoding as a quoted Go string
// literal, for embedding in a mustDecodeBody call.
func fallbackJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		// v was itself decoded from JSON moments ago, so re-encoding it
		// cannot fail; this is unreachable in practice.
		return fmt.Sprintf("%q", "null")
	}

	return fmt.Sprintf("%q", string(data))
}
