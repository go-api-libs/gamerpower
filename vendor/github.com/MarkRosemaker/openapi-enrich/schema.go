package enrich

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
	"uuid"

	"github.com/MarkRosemaker/openapi"
	"github.com/MarkRosemaker/openapi-enrich/cassette"
	merge "github.com/MarkRosemaker/openapi-merge"
	apitypes "github.com/go-api-libs/types"
)

// newSchemaFromJSON infers an OpenAPI schema from a JSON-encoded value.
func newSchemaFromJSON(data []byte) (*openapi.Schema, error) {
	return decodeSchema(jsontext.NewDecoder(bytes.NewReader(data)))
}

func decodeSchema(dec *jsontext.Decoder) (*openapi.Schema, error) {
	v, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}

	switch v.Kind() {
	case '"': // string
		s := &openapi.Schema{
			Type:   openapi.TypeString,
			Format: stringFormat(v.String()),
		}

		// A redacted value makes a poor example: it says nothing the inferred
		// format does not already say, and reads as though the API returns
		// asterisks. Masking is shape-preserving, so the format survives without
		// it.
		if !cassette.IsMasked(v.String()) {
			s.Example = jsontext.Value(fmt.Appendf(nil, "%q", v.String()))
		}

		return s, nil

	case '0': // number
		str := v.String()
		if _, err := strconv.Atoi(str); err == nil {
			return &openapi.Schema{Type: openapi.TypeInteger, Example: jsontext.Value(str)}, nil
		}

		return &openapi.Schema{Type: openapi.TypeNumber, Format: openapi.FormatDouble, Example: jsontext.Value(str)}, nil

	case 't': // true
		return &openapi.Schema{Type: openapi.TypeBoolean}, nil
	case 'f': // false
		return &openapi.Schema{Type: openapi.TypeBoolean}, nil
	case 'n': // null → object placeholder; isGeneratedFromNull in openapi-merge checks Example=="null"
		// TODO: perhaps it would be better to actually set TypeNull here
		return &openapi.Schema{Type: openapi.TypeObject, Example: jsontext.Value("null")}, nil
	case '{': // begin object
		return decodeObjectSchema(dec)
	case '[': // begin array
		return decodeArraySchema(dec)
	default:
		return nil, fmt.Errorf("unexpected token type %s", v.Kind())
	}
}

func decodeObjectSchema(dec *jsontext.Decoder) (*openapi.Schema, error) {
	type kv struct {
		key    string
		schema *openapi.Schema
	}

	var pairs []kv

	for dec.PeekKind() != '}' {
		keyTok, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}

		if keyTok.Kind() != '"' {
			return nil, fmt.Errorf("expected string key, got %s", keyTok)
		}

		key := keyTok.String()

		propSchema, err := decodeSchema(dec)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", key, err)
		}

		// A redacted number is indistinguishable from a real one by value, so
		// drop the example by key instead. These keys hold credentials, which
		// have no business appearing as examples either way.
		if cassette.RedactsBodyKey(key) {
			propSchema.Example = nil
		}

		pairs = append(pairs, kv{key, propSchema})
	}

	if _, err := dec.ReadToken(); err != nil { // consume '}'
		return nil, err
	}

	if len(pairs) == 0 {
		return &openapi.Schema{Type: openapi.TypeObject}, nil
	}

	// If every key is a stringified non-negative integer the object is a
	// numeric-keyed map (e.g. {"0":{...},"1":{...}}). Model it with
	// additionalProperties so the key pattern is explicit and the schema stays
	// compact regardless of how many entries are present.
	allNumeric := true
	for _, p := range pairs {
		if !isNumericKey(p.key) {
			allNumeric = false
			break
		}
	}

	if allNumeric {
		var valueSchema *openapi.Schema
		for _, p := range pairs {
			if valueSchema == nil {
				valueSchema = p.schema
				continue
			}

			if err := merge.Schema(valueSchema, p.schema, false); err != nil {
				return nil, fmt.Errorf("merging additionalProperties value: %w", err)
			}
		}

		return &openapi.Schema{
			Type:                 openapi.TypeObject,
			AdditionalProperties: &openapi.SchemaRef{Value: valueSchema},
		}, nil
	}

	// Named properties — build the schema as before.
	s := &openapi.Schema{
		Type:       openapi.TypeObject,
		Properties: openapi.SchemaRefs{},
	}
	for _, p := range pairs {
		s.Properties.Set(p.key, &openapi.SchemaRef{Value: p.schema})
		s.Required = append(s.Required, p.key)
	}

	return s, nil
}

// isNumericKey reports whether s consists entirely of ASCII digits.
func isNumericKey(s string) bool {
	if len(s) == 0 {
		return false
	}

	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

func decodeArraySchema(dec *jsontext.Decoder) (*openapi.Schema, error) {
	s := &openapi.Schema{Type: openapi.TypeArray}

	var elems []*openapi.Schema
	for dec.PeekKind() != ']' {
		elem, err := decodeSchema(dec)
		if err != nil {
			return nil, err
		}

		elems = append(elems, elem)
	}

	if _, err := dec.ReadToken(); err != nil { // consume ']'
		return nil, err
	}

	if len(elems) == 0 {
		// empty array → placeholder object items, refined on non-empty array
		s.Items = &openapi.SchemaRef{Value: &openapi.Schema{Type: openapi.TypeObject, Example: jsontext.Value("null")}}
		return s, nil
	}

	if item, ok := mergeHomogeneous(elems); ok {
		s.Items = &openapi.SchemaRef{Value: item}
		return s, nil
	}

	// elems can't merge into one schema: a fixed-size, positionally-typed
	// array (e.g. OpenSky's state vectors: [icao24 string, ..., time_position
	// int, ..., on_ground bool, ...]) mixes types by position, which
	// prefixItems -- not items -- is meant to describe.
	s.PrefixItems = make(openapi.SchemaRefList, len(elems))
	for i, elem := range elems {
		s.PrefixItems[i] = &openapi.SchemaRef{Value: elem}
	}

	return s, nil
}

// mergeHomogeneous attempts to merge every element into a single schema,
// succeeding whenever they describe a plain, arbitrary-length list; ok is
// false the moment two elements' types genuinely disagree. It works on
// clones throughout: decodeArraySchema needs elems left untouched to fall
// back to prefixItems on failure, and merge.Schema mutates both of its
// arguments even when it ultimately returns an error partway through.
func mergeHomogeneous(elems []*openapi.Schema) (_ *openapi.Schema, ok bool) {
	item, err := cloneSchema(elems[0])
	if err != nil {
		return nil, false
	}

	for _, elem := range elems[1:] {
		clone, err := cloneSchema(elem)
		if err != nil {
			return nil, false
		}

		if err := merge.Schema(item, clone, false); err != nil {
			return nil, false
		}
	}

	return item, true
}

// cloneSchema deep-copies s via a JSON round-trip.
func cloneSchema(s *openapi.Schema) (*openapi.Schema, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}

	clone := &openapi.Schema{}
	if err := json.Unmarshal(data, clone); err != nil {
		return nil, err
	}

	return clone, nil
}

// stringFormat detects the special format for a string value.
// It tries in order: UUID, URI, Email, DateTime (RFC3339), IPv4/IPv6.
func stringFormat(s string) openapi.Format {
	if isUUID(s) {
		return openapi.FormatUUID
	}

	if u, err := url.Parse(s); err == nil && u.Scheme != "" && u.Host != "" {
		return openapi.FormatURI
	}

	if apitypes.Email(s).Validate() == nil {
		return openapi.FormatEmail
	}

	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return openapi.FormatDateTime
	}

	if ip := net.ParseIP(s); ip != nil {
		if ip.To4() != nil {
			return openapi.FormatIPv4
		}

		return openapi.FormatIPv6
	}

	return ""
}

// isUUID reports whether s matches the UUID format.
func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
