package ir

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
	"github.com/ettle/strcase"
)

// SchemaRefGoType returns the Go type string for a SchemaRef.
// After flattening, complex schemas are moved to components and referenced by $ref;
// this function extracts the component name from the identifier or maps the inline type.
func SchemaRefGoType(ref *openapi.SchemaRef) (*GoType, error) {
	if ref.Ref != nil {
		// A date-time-or-int oneOf collapses to time.Time even when reached
		// via $ref, so no synthetic component type name leaks into the
		// generated code. A oneOf/anyOf union otherwise resolves to its own
		// generated pointer-bag type (see fromUnionSchema), so the ordinary
		// $ref-name resolution below already does the right thing for it.
		if ref.Value != nil && isDateTimeOrIntegerOneOf(ref.Value) {
			return &GoType{Name: "time.Time"}, nil
		}
		// "#/components/schemas/Name" → "Name"
		parts := strings.Split(ref.Ref.Identifier, "/")
		name := parts[len(parts)-1]

		// The named type this $ref points at is itself array-kind (e.g.
		// "type TimeEntries []TimeEntry"), so it's already nilable on its
		// own -- see [GoType.IsNilable].
		isNilable := ref.Value != nil && ref.Value.Type == openapi.TypeArray

		return &GoType{Name: name, IsNilable: isNilable}, nil
	}

	return SchemaGoType(ref.Value)
}

// SchemaGoType maps an openapi.Schema to its Go type string.
func SchemaGoType(s *openapi.Schema) (*GoType, error) {
	switch s.Type {
	case openapi.TypeBoolean:
		return &GoType{Name: "bool"}, nil
	case openapi.TypeInteger:
		return integerGoType(s.Format)
	case openapi.TypeNumber:
		return numberGoType(s.Format)
	case openapi.TypeString:
		return stringGoType(s.Format)
	case openapi.TypeArray:
		return arrayGoType(s)
	case openapi.TypeObject:
		return objectGoType(s)
	case "":
		if isDateTimeOrIntegerOneOf(s) {
			return &GoType{Name: "time.Time"}, nil
		}
		// A oneOf/anyOf union normally goes through fromSchema as a named
		// component and gets a real generated pointer-bag type (see
		// fromUnionSchema). This is only reached for a union with no name to
		// give it (e.g. inline within array items or additionalProperties),
		// where there's nothing to generate a struct for.
		if isAnyOfOnly(s) || isOneOfOnly(s) {
			return &GoType{Name: "any"}, nil
		}

		return nil, fmt.Errorf("unsupported schema type: %q", s.Type)
	default:
		return nil, fmt.Errorf("unsupported schema type: %q", s.Type)
	}
}

// isDateTimeOrIntegerOneOf reports whether s is a oneOf composition of exactly
// two schemas: one string with format date-time, and one integer. The order of
// the two entries does not matter. When true, callers should surface a
// time.Time typed field with a custom (un)marshaller in the generated code.
func isDateTimeOrIntegerOneOf(s *openapi.Schema) bool {
	if len(s.OneOf) != 2 {
		return false
	}

	var hasDateTime, hasInteger bool
	for _, entry := range s.OneOf {
		if entry == nil || entry.Value == nil {
			return false
		}

		v := entry.Value
		switch v.Type {
		case openapi.TypeString:
			if v.Format == openapi.FormatDateTime {
				hasDateTime = true
			}
		case openapi.TypeInteger:
			hasInteger = true
		default:
		}
	}

	return hasDateTime && hasInteger
}

// isAnyOfOnly reports whether s is an untagged union expressed purely via
// anyOf (no type, allOf, or oneOf of its own).
func isAnyOfOnly(s *openapi.Schema) bool {
	return s.Type == "" && len(s.AnyOf) > 0 && len(s.AllOf) == 0 && len(s.OneOf) == 0
}

// isOneOfOnly reports whether s is an untagged union expressed purely via
// oneOf (no type, allOf, or anyOf of its own), and is not the
// date-time-or-integer pattern that collapses to time.Time.
func isOneOfOnly(s *openapi.Schema) bool {
	return s.Type == "" && len(s.OneOf) > 0 && len(s.AllOf) == 0 && len(s.AnyOf) == 0 &&
		!isDateTimeOrIntegerOneOf(s)
}

func integerGoType(f openapi.Format) (*GoType, error) {
	switch f {
	case "":
		return &GoType{Name: "int"}, nil
	case openapi.FormatInt32:
		return &GoType{Name: "int32"}, nil
	case openapi.FormatInt64:
		return &GoType{Name: "int64"}, nil
	case openapi.FormatUint:
		return &GoType{Name: "uint"}, nil
	case openapi.FormatUint32:
		return &GoType{Name: "uint32"}, nil
	case openapi.FormatUint64:
		return &GoType{Name: "uint64"}, nil
	case openapi.FormatDuration:
		return &GoType{Name: "time.Duration"}, nil
	case openapi.FormatDateTime:
		return &GoType{Name: "time.Time"}, nil
	default:
		return nil, fmt.Errorf("unsupported integer format: %q", f)
	}
}

func numberGoType(f openapi.Format) (*GoType, error) {
	switch f {
	case "", openapi.FormatDouble:
		return &GoType{Name: "float64"}, nil
	case openapi.FormatFloat:
		return &GoType{Name: "float32"}, nil
	default:
		return nil, fmt.Errorf("unsupported number format: %q", f)
	}
}

func stringGoType(f openapi.Format) (*GoType, error) {
	switch f {
	case "", openapi.FormatPassword, openapi.FormatByte, openapi.FormatBinary, openapi.FormatZipCode:
		return &GoType{Name: "string"}, nil
	case openapi.FormatUUID:
		return &GoType{Name: "uuid.UUID"}, nil
	case openapi.FormatURI, openapi.FormatURIRef:
		return &GoType{Name: "url.URL"}, nil
	case openapi.FormatEmail:
		return &GoType{Name: "types.Email"}, nil
	case openapi.FormatDateTime:
		return &GoType{Name: "time.Time"}, nil
	case openapi.FormatDate:
		return &GoType{Name: "civil.Date"}, nil
	case openapi.FormatIPv4, openapi.FormatIPv6:
		return &GoType{Name: "net.IP"}, nil
	default:
		return nil, fmt.Errorf("unsupported string format: %q", f)
	}
}

func arrayGoType(s *openapi.Schema) (*GoType, error) {
	if s.Items == nil {
		return &GoType{Name: "[]any"}, nil
	}

	tp, err := SchemaRefGoType(s.Items)
	if err != nil {
		return nil, fmt.Errorf("items: %w", err)
	}

	if s.MinItems > 0 && s.MaxItems != nil && s.MinItems == *s.MaxItems {
		tp.IsArrayOfSize = int(s.MinItems)
	} else {
		tp.IsSlice = true
	}

	return tp, nil
}

func objectGoType(s *openapi.Schema) (*GoType, error) {
	if s.AdditionalProperties != nil {
		tp, err := SchemaRefGoType(s.AdditionalProperties)
		if err != nil {
			return nil, fmt.Errorf("additionalProperties: %w", err)
		}

		return &GoType{Name: "map[string]" + tp.Name}, nil
	}

	// Named objects with properties are moved to components by the flatten pass.
	return &GoType{Name: "struct{}"}, nil
}

// FromComponentSchemas converts a set of named component schemas to IR schemas.
func FromComponentSchemas(schemas openapi.Schemas) ([]Schema, error) {
	result := make([]Schema, 0, len(schemas))
	for name, s := range schemas.ByIndex() {
		if override := goNameOverride(s); override != "" {
			name = override
		}

		irSchema, err := fromSchema(name, s)
		if err != nil {
			return nil, fmt.Errorf("schema %q: %w", name, err)
		}

		if irSchema != nil {
			result = append(result, *irSchema)
		}
	}

	return result, nil
}

func fromSchema(name string, s *openapi.Schema) (*Schema, error) {
	switch s.Type {
	case openapi.TypeObject:
		if s.AdditionalProperties != nil {
			mapValueType, err := SchemaRefGoType(s.AdditionalProperties)
			if err != nil {
				return nil, err
			}

			// A map of strings gets a named type like any other map: a
			// property referencing this component resolves to the component's
			// name, so declining to declare it leaves that name undefined.
			return &Schema{
				Name:        name,
				Description: getDescription(s, name),
				Kind:        SchemaKindMap,
				MapKey:      "string",
				MapValue:    mapValueType.String(),
			}, nil
		}

		return fromObjectSchema(name, s)
	case openapi.TypeString, openapi.TypeInteger, openapi.TypeNumber, openapi.TypeBoolean:
		if len(s.Enum) > 0 {
			return fromEnumSchema(name, s)
		}

		return fromScalarSchema(name, s)
	case openapi.TypeArray:
		if len(s.PrefixItems) > 0 {
			return fromTupleSchema(name, s)
		}

		return fromArraySchema(name, s)
	case "":
		if isDateTimeOrIntegerOneOf(s) {
			return nil, nil // handled specially: SchemaRefGoType resolves the $ref straight to time.Time
		}

		if len(s.AllOf) > 0 {
			return fromAllOfSchema(name, s)
		}

		if len(s.OneOf) > 0 {
			return fromUnionSchema(name, s, true)
		}

		if len(s.AnyOf) > 0 {
			return fromUnionSchema(name, s, false)
		}

		return nil, nil
	default:
		return nil, nil // scalar types are used inline
	}
}

// fromUnionSchema builds a pointer-bag union type from an untagged oneOf or
// anyOf composition: one nilable field per variant, with no discriminator.
// The caller distinguishes which variant matched by checking which field is
// non-nil after unmarshaling.
func fromUnionSchema(name string, s *openapi.Schema, isOneOf bool) (*Schema, error) {
	variants := s.AnyOf
	if isOneOf {
		variants = s.OneOf
	}

	counts := make(map[string]int, len(variants))
	unionVariants := make([]UnionVariant, len(variants))
	for i, v := range variants {
		tp, err := SchemaRefGoType(v)
		if err != nil {
			return nil, fmt.Errorf("variant %d: %w", i, err)
		}

		base := unionVariantFieldName(tp, i)

		counts[base]++
		fieldName := base
		if n := counts[base]; n > 1 {
			fieldName = fmt.Sprintf("%s%d", base, n)
		}

		unionVariants[i] = UnionVariant{
			FieldName: fieldName,
			Type:      tp.String(),
		}
	}

	return &Schema{
		Name:          name,
		Description:   getDescription(s, name),
		Kind:          SchemaKindUnion,
		UnionVariants: unionVariants,
		IsOneOf:       isOneOf,
	}, nil
}

// unionVariantFieldName derives an exported Go field name from a union
// variant's resolved type, e.g. "Card" for *Card, "UUID" for uuid.UUID.
func unionVariantFieldName(t *GoType, index int) string {
	name := t.Name
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:] // strip package qualifier, e.g. "uuid.UUID" -> "UUID"
	}

	name = strcase.ToGoPascal(name)
	if name == "" {
		name = fmt.Sprintf("Variant%d", index+1)
	}

	return name
}

func getDescription(s *openapi.Schema, name string) string {
	if s.Description != "" {
		return s.Description
	}

	return fmt.Sprintf("%s defines a model", name)
}

// goNameOverride returns the schema's x-go-name extension, or "" if unset.
// See https://github.com/oapi-codegen/oapi-codegen/blob/main/docs/extensions.md#x-go-name.
func goNameOverride(s *openapi.Schema) string {
	if len(s.Extensions) == 0 {
		return ""
	}

	var ext struct {
		GoName string `json:"x-go-name"`
	}
	if err := json.Unmarshal(s.Extensions, &ext); err != nil {
		return ""
	}

	return ext.GoName
}

func fromAllOfSchema(name string, s *openapi.Schema) (*Schema, error) {
	requiredSet := make(map[string]bool)
	for _, r := range s.Required {
		requiredSet[r] = true
	}

	var fields []Field
	for _, entry := range s.AllOf {
		if entry.Ref != nil {
			// $ref entry → embedded struct
			typeName, err := SchemaRefGoType(entry)
			if err != nil {
				return nil, err
			}

			fields = append(fields, Field{
				Type:     typeName.String(),
				Embedded: true,
			})

			continue
		}

		if entry.Value == nil {
			continue
		}

		// inline object entry → merge its properties as regular fields
		for _, r := range entry.Value.Required {
			requiredSet[r] = true
		}

		for jsonName, propRef := range entry.Value.Properties.ByIndex() {
			field, err := getField(jsonName, propRef, requiredSet)
			if err != nil {
				return nil, fmt.Errorf("allOf property %q: %w", jsonName, err)
			}

			fields = append(fields, field)
		}
	}

	return &Schema{
		Name:        name,
		Description: getDescription(s, name),
		Kind:        SchemaKindAllOf,
		Fields:      fields,
	}, nil
}

func getField(jsonName string, propRef *openapi.SchemaRef, requiredSet map[string]bool) (Field, error) {
	goType, err := SchemaRefGoType(propRef)
	if err != nil {
		return Field{}, err
	}

	v := propRef.Value

	required := requiredSet[jsonName]
	if !required {
		switch v.Type {
		case openapi.TypeBoolean, openapi.TypeArray:
		case openapi.TypeString:
			switch v.Format {
			case openapi.FormatURI, openapi.FormatUUID:
				goType.IsPointer = true
			default:
			}
		default:
			goType.IsPointer = true
		}
	}

	ref := cmp.Or(propRef.Ref, &openapi.Reference{})

	fieldName := fieldGoName(jsonName)
	// only from an inline schema: a $ref'd component's own x-go-name renames
	// that type, not every field that happens to reference it.
	if propRef.Ref == nil {
		if override := goNameOverride(v); override != "" {
			fieldName = override
		}
	}

	return Field{
		Name:            fieldName,
		JSONName:        jsonName,
		Type:            goType.String(),
		JSONTag:         buildJSONTag(jsonName, v.Type, v.Format, required),
		Description:     cmp.Or(ref.Description, v.Description),
		Required:        required,
		IsDateTimeOrInt: isDateTimeOrIntegerOneOf(v),
		IsUnixTime:      goType.Name == "time.Time" && v.Type == openapi.TypeInteger,
	}, nil
}

func fromObjectSchema(name string, s *openapi.Schema) (*Schema, error) {
	requiredSet := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		requiredSet[r] = true
	}

	fields := make([]Field, 0, len(s.Properties))
	for jsonName, propRef := range s.Properties.ByIndex() {
		field, err := getField(jsonName, propRef, requiredSet)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", jsonName, err)
		}

		fields = append(fields, field)
	}

	return &Schema{
		Name:        name,
		Description: getDescription(s, name),
		Kind:        SchemaKindStruct,
		Fields:      fields,
	}, nil
}

func fromEnumSchema(name string, s *openapi.Schema) (*Schema, error) {
	tp, err := enumBaseGoType(s.Type, s.Format)
	if err != nil {
		return nil, err
	}

	values := make([]EnumValue, len(s.Enum))
	for i, v := range s.Enum {
		display, literal, err := formatEnumValue(v, s.Type)
		if err != nil {
			return nil, fmt.Errorf("enum[%d]: %w", i, err)
		}

		values[i] = EnumValue{
			GoName:  enumConstName(name, display),
			Value:   display,
			Literal: literal,
		}
	}

	return &Schema{
		Name:        name,
		Description: getDescription(s, name),
		Kind:        SchemaKindEnum,
		Type:        tp.String(),
		EnumValues:  values,
	}, nil
}

// enumBaseGoType returns the underlying Go type for an enum's declared schema type.
func enumBaseGoType(t openapi.DataType, f openapi.Format) (*GoType, error) {
	switch t {
	case openapi.TypeString:
		return stringGoType(f)
	case openapi.TypeInteger:
		return integerGoType(f)
	case openapi.TypeNumber:
		return numberGoType(f)
	case openapi.TypeBoolean:
		return &GoType{Name: "bool"}, nil
	default:
		return nil, fmt.Errorf("unsupported enum type: %q", t)
	}
}

// formatEnumValue converts a raw enum member (decoded from JSON as string,
// float64, or bool per the schema's declared type) into its human-readable
// display form and its Go source literal.
func formatEnumValue(v jsontext.Value, t openapi.DataType) (display, literal string, err error) {
	switch t {
	case openapi.TypeString:
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return "", "", fmt.Errorf("unmarshalling %q into a string: %w", v, err)
		}

		return s, strconv.Quote(s), nil
	case openapi.TypeInteger:
		var i int64
		if err := json.Unmarshal(v, &i); err != nil {
			return "", "", fmt.Errorf("unmarshalling %q into a int64: %w", v, err)
		}

		s := strconv.FormatInt(i, 10)

		return s, s, nil
	case openapi.TypeNumber:
		var f float64
		if err := json.Unmarshal(v, &f); err != nil {
			return "", "", fmt.Errorf("unmarshalling %q into a float64: %w", v, err)
		}

		s := strconv.FormatFloat(f, 'g', -1, 64)

		return s, s, nil
	case openapi.TypeBoolean:
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			return "", "", fmt.Errorf("unmarshalling %q into a bool: %w", v, err)
		}

		s := strconv.FormatBool(b)

		return s, s, nil
	default:
		return "", "", fmt.Errorf("unsupported enum type: %q", t)
	}
}

func fromArraySchema(name string, s *openapi.Schema) (*Schema, error) {
	aliasType, err := arrayGoType(s)
	if err != nil {
		return nil, err
	}

	return &Schema{
		Name:        name,
		Description: getDescription(s, name),
		Kind:        SchemaKindAlias,
		Type:        aliasType.String(),
	}, nil
}

// fromTupleSchema declares a named component for a fixed-length,
// positionally-typed array (JSON Schema's prefixItems): a struct field per
// position, since Go has no tuple type and each position has its own type
// throughout every sample -- not one shared item type like a plain array.
func fromTupleSchema(name string, s *openapi.Schema) (*Schema, error) {
	width := len(strconv.Itoa(len(s.PrefixItems) - 1))

	fields := make([]Field, len(s.PrefixItems))
	for i, p := range s.PrefixItems {
		tp, err := SchemaRefGoType(p)
		if err != nil {
			return nil, &errpath.ErrField{Field: "prefixItems", Err: &errpath.ErrIndex{Index: i, Err: err}}
		}

		fieldName := fmt.Sprintf("Item%0*d", width, i)
		// only from an inline schema, same as an object property: a $ref'd
		// position's own x-go-name renames that type, not this position.
		if p.Ref == nil {
			if override := goNameOverride(p.Value); override != "" {
				fieldName = override
			}
		}

		fields[i] = Field{
			Name: fieldName,
			Type: tp.String(),
		}
	}

	return &Schema{
		Name:        name,
		Description: getDescription(s, name),
		Kind:        SchemaKindTuple,
		Fields:      fields,
	}, nil
}

// fromScalarSchema declares a named component that is a plain scalar.
//
// The declaration is what makes the name usable: a $ref to this component
// resolves to its name, and a response body decoded into it can carry an Error
// method, which a bare string or int cannot.
func fromScalarSchema(name string, s *openapi.Schema) (*Schema, error) {
	aliasType, err := SchemaGoType(s)
	if err != nil {
		return nil, err
	}

	return &Schema{
		Name:        name,
		Description: getDescription(s, name),
		Kind:        SchemaKindAlias,
		Type:        aliasType.String(),
	}, nil
}

// fieldGoName converts a JSON property name to an exported Go identifier.
func fieldGoName(jsonName string) string {
	// special case
	if strings.ToLower(jsonName) == "pdf" {
		return "PDF"
	}

	if suffix, ok := strings.CutPrefix(jsonName, "_"); ok {
		jsonName = fmt.Sprintf("Underscore %s", suffix)
	}

	// replace special characters before PascalCasing
	r := strings.NewReplacer(
		"+", " Plus ",
		".", " Dot ",
		"/", " ",
		"(", "",
		")", "",
		"C#", "CSharp",
		"F#", "CSharp",
	)

	sanitized := r.Replace(jsonName)
	sanitized = replaceLeadingDigits(sanitized)
	name := strcase.ToGoPascal(sanitized)

	// "Error" collides with the built-in error interface's Error() string
	// method (a struct can't have both a field and a method named Error),
	// so callers can never make the generated type satisfy error. Rename
	// the field; the json tag still uses the original JSON name.
	if name == "Error" {
		return "Err"
	}

	return name
}

var replInvalidChars = strings.NewReplacer(
	"#", " Sharp ",
	"/", " ",
	"+", " Plus ",
	".", " Dot ",
	"(", "",
	")", "",
	":", "",
)

// enumConstName builds the Go constant name for an enum value, e.g. MyEnum + "foo_bar" → MyEnumFooBar.
func enumConstName(typeName, value string) string {
	sanitized := replInvalidChars.Replace(value)
	sanitized = replaceLeadingDigits(sanitized)

	if len(sanitized) <= 3 && sanitized == strings.ToUpper(sanitized) {
		return typeName + sanitized
	}

	return typeName + strcase.ToGoPascal(sanitized)
}

// replaceLeadingDigits converts every leading digit in the first word to its
// word equivalent, so the result can be used as a Go identifier.
// e.g. "4K" → "Four K", "1080p" → "One Zero Eight Zero p".
func replaceLeadingDigits(s string) string {
	if s == "" {
		return s
	}

	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}

	first := []rune(words[0])
	if len(first) == 0 || !unicode.IsDigit(first[0]) {
		return s
	}

	var parts []string

	i := 0
	for i < len(first) && unicode.IsDigit(first[i]) {
		parts = append(parts, digitWord(first[i]))
		i++
	}

	parts = append(parts, string(first[i:]))
	words[0] = strings.Join(parts, " ")

	return strings.Join(words, " ")
}

var digitWords = [10]string{
	"Zero", "One", "Two", "Three", "Four",
	"Five", "Six", "Seven", "Eight", "Nine",
}

func digitWord(r rune) string {
	d := int(r - '0')
	if d >= 0 && d < len(digitWords) {
		return digitWords[d]
	}

	return string(r)
}

// buildJSONTag computes the json struct tag for a field.
//
// Rules (mirroring the apilib reference):
//   - plain string, required:     json:"name"
//   - plain string, optional:     json:"name"       (empty strings are valid values)
//   - array:                      json:"name,omitempty"
//   - other, required:            json:"name"        (a required field must always be sent,
//     zero value included -- omitzero would silently drop e.g. a required "false" or "0")
//   - other, optional:            json:"name,omitempty"
func buildJSONTag(jsonName string, tp openapi.DataType, format openapi.Format, required bool) string {
	// NOTE: JSON tags need to be rethought;
	// ideally, we want to not marshal unnecessarily
	// at the same time, sometimes we need to marshal null to delete values
	// we may need to decide based on custom x- tags in the openapi spec
	var opts string
	switch tp {
	case openapi.TypeString:
		switch format {
		case "": // regular string
			opts = ",omitzero"
		default:
			// NOTE: copied from legacy code, might not make sense
			if required {
				opts = ",omitzero"
			} else {
				opts = ",omitempty"
			}
		}
	case openapi.TypeArray:
		// omitempty would drop an initialised-but-empty slice, so a required
		// array could never be sent as []. omitzero drops only a nil slice,
		// leaving the sender to choose: nil omits the field, []T{} sends [].
		// If the array is required, we do not have any tags.
		if !required {
			opts = ",omitzero"
		}
	case openapi.TypeBoolean, openapi.TypeInteger:
		// A required field must always be sent, zero value included: a
		// required boolean that happens to be false, or a required count of
		// 0, is a real value, not an absence to omit.
		if !required {
			opts = ",omitempty"
		}
	default:
		if !required {
			opts = ",omitempty"
		}
	}

	return fmt.Sprintf(`json:"%s%s"`, jsonName, opts)
}
