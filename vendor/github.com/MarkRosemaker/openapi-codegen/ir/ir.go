package ir

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Document is the top-level IR type passed to templates.
type Document struct {
	// If enabled, debug mode will record responses that failed to unmarshal.
	Debug                  bool        `json:"debug,omitzero"`
	Title                  string      `json:"title,omitzero"`
	Production             bool        `json:"production,omitzero"`
	PackageName            string      `json:"packageName,omitzero"`
	BaseURL                URLParts    `json:"baseURL,omitzero"`
	UserAgent              string      `json:"userAgent,omitzero"`
	Operations             []Operation `json:"operations,omitempty"`
	GlobalParams           Params      `json:"globalParams,omitempty"`
	Schemas                []Schema    `json:"schemas,omitempty"`
	Auth                   Auth        `json:"security,omitzero"`
	HasURLFields           bool        `json:"hasURLFields,omitzero"`
	HasDurationFields      bool        `json:"hasDurationFields,omitzero"`
	HasDateFields          bool        `json:"hasDateFields,omitzero"`
	HasDateTimeOrIntFields bool        `json:"hasDateTimeOrIntFields,omitzero"`

	// HasServerOverrides is true when any path item names a server of its own,
	// which is what the generated serverURL helper is for.
	HasServerOverrides bool `json:"hasServerOverrides,omitzero"`

	// InteractionCalls holds one entry per matched interaction.
	// Populated at code-gen time; not serialized to ir.json (too noisy).
	InteractionCalls InteractionCalls `json:"-"`
}

func (d Document) NeedMustDecodeBody() bool {
	return d.InteractionCalls.UseMustDecodeBody()
}

type InteractionCalls []InteractionCall

func (ics InteractionCalls) UseMustDecodeBody() bool {
	for _, ic := range ics {
		if ic.UsesMustDecodeBody() {
			return true
		}
	}

	return false
}

// InteractionCall is one operation call extracted from a recorded interaction.
type InteractionCall struct {
	Op         *Operation         // matched operation
	PathArgs   []string           // Go literal per path param, same order as Op.PathParams
	QueryArgs  []InteractionParam // set query params only (omitted = use nil params)
	HeaderArgs []InteractionParam // set query params only (omitted = use nil params)
	// IsSuccess is true when StatusCode matches one of Op's declared success responses.
	IsSuccess bool
	// ErrorType is the Go type name of the declared error response schema for
	// StatusCode, empty when the operation has no schema for that status (the
	// client falls back to a generic status-string error in that case).
	ErrorType string
	// BodyLiteral is a Go expression for the recorded request body, set
	// whenever Op.RequestBody is non-nil: a composite literal of the
	// request body's type when every field could be expressed that way,
	// falling back to a runtime JSON decode (mustDecodeBody) for values a
	// literal can't cleanly represent. Either way the replayed request
	// carries the same body the interaction was recorded with, instead of
	// a zero value.
	BodyLiteral string
}

func (ic InteractionCall) UsesMustDecodeBody() bool {
	return strings.HasPrefix(ic.BodyLiteral, "mustDecodeBody")
}

// InteractionParam is one query param with its Go literal value.
type InteractionParam struct {
	FieldName string // PascalCase field name on the params struct
	Literal   string // Go expression, e.g. `3` or `"abc"`
}

// URLParts holds a decomposed server URL.
type URLParts struct {
	Scheme string `json:"scheme,omitzero"`
	Host   string `json:"host,omitzero"`
	Path   string `json:"path,omitzero"`
}

// Operation represents a single API operation.
type Operation struct {
	// BaseURL is set when the path item names a server of its own, overriding
	// the document's for this operation only.
	BaseURL         *URLParts `json:"baseURL,omitzero"`
	Name            string    `json:"name,omitzero"`
	Description     string    `json:"description,omitzero"`
	Summary         string    `json:"summary,omitzero"`
	Method          string    `json:"method,omitzero"`
	PathTemplate    string    `json:"pathTemplate,omitzero"`
	JoinPathArgs    []string  `json:"joinPathArgs,omitempty"`
	PathParams      Params    `json:"pathParams,omitempty"`
	QueryParams     Params    `json:"queryParams,omitempty"`
	HeaderParams    Params    `json:"headerParams,omitempty"`
	HasParams       bool      `json:"hasParams,omitzero"`
	ParamStructName string    `json:"paramStructName,omitzero"`
	RequestBody     *ReqBody  `json:"requestBody,omitempty"`
	Responses       Responses `json:"responses,omitempty"`
	SuccessReturn   *GoType   `json:"successReturn,omitempty"`
	Deprecated      bool      `json:"deprecated,omitzero"`
	// RawBytesSuccess is true when the operation's success response has no
	// JSON media type, so SuccessReturn is a raw []byte read directly from
	// the response body rather than a JSON-decoded type. Such operations
	// are generated as a single concrete method, not a generic function.
	RawBytesSuccess bool `json:"rawBytesSuccess,omitzero"`
}

func (op Operation) ParamsInStruct() Params {
	return append(op.QueryParams, op.HeaderParams...)
}

func (op Operation) NilParamsExpr() string {
	params := op.ParamsInStruct()
	if len(params) == 0 {
		return ""
	}

	if params.Required() {
		return fmt.Sprintf("%s{}", op.ParamStructName)
	}

	return "nil"
}

// JSPathTemplate returns the path template with {jsonName} placeholders replaced
// by ${goName} JavaScript template-literal interpolations.
func (op Operation) JSPathTemplate() string {
	result := op.PathTemplate
	for _, p := range op.PathParams {
		result = strings.ReplaceAll(result, "{"+p.JSONName+"}", "${"+p.GoName+"}")
	}

	return result
}

// Schema represents a named component schema.
type Schema struct {
	Name        string      `json:"name,omitzero"`
	Description string      `json:"description,omitzero"`
	Kind        SchemaKind  `json:"kind,omitzero"`
	Type        string      `json:"type,omitzero"`
	Fields      []Field     `json:"fields,omitempty"`
	EnumValues  []EnumValue `json:"enumValues,omitempty"`
	MapKey      string      `json:"mapKey,omitzero"`
	MapValue    string      `json:"mapValue,omitzero"`

	// UnionVariants is set for SchemaKindUnion: one pointer field per
	// oneOf/anyOf variant. IsOneOf selects the cardinality rule enforced by
	// the generated UnmarshalJSONFrom: exactly one variant must match for
	// oneOf, at least one for anyOf.
	UnionVariants []UnionVariant `json:"unionVariants,omitempty"`
	IsOneOf       bool           `json:"isOneOf,omitzero"`
}

// SchemaKind categorizes a schema into struct, enum, or array alias.
type SchemaKind int

const (
	SchemaKindStruct SchemaKind = iota // object with properties
	SchemaKindEnum                     // string with enum values
	SchemaKindAlias                    // named type alias: array or plain scalar
	SchemaKindAllOf                    // allOf composition (struct with embedded types)
	SchemaKindMap
	SchemaKindUnion // untagged oneOf/anyOf composition (pointer-bag struct)
)

// UnionVariant is one member of a SchemaKindUnion's pointer bag.
type UnionVariant struct {
	// FieldName is the exported Go field name, derived from the variant's
	// resolved type name (e.g. "Card" for a field of type *Card).
	FieldName string `json:"fieldName,omitzero"`
	// Type is the variant's own Go type, without the pointer the field adds.
	Type string `json:"type,omitzero"`
}

// Field is a named field within a struct schema.
type Field struct {
	Name        string `json:"name,omitzero"`
	JSONName    string `json:"jsonName,omitzero"`
	Type        string `json:"type,omitzero"`
	JSONTag     string `json:"jsonTag,omitzero"`
	Description string `json:"description,omitzero"`
	Required    bool   `json:"required,omitzero"`
	Embedded    bool   `json:"embedded,omitzero"` // true for allOf $ref entries rendered as embedded structs

	// IsDateTimeOrInt is true when the property's schema is a oneOf of a
	// date-time string and an integer. The Go type is time.Time, but a custom
	// (un)marshaller is required to accept either form on the wire.
	IsDateTimeOrInt bool `json:"isDateTimeOrInt,omitzero"`
}

// EnumValue is one member of an enum type.
type EnumValue struct {
	GoName string `json:"goName,omitzero"`
	// Value is the human-readable string form of the enum member, e.g. "active" or "3.14".
	Value string `json:"value,omitzero"`
	// Literal is the Go source literal to embed in the generated constant,
	// e.g. `"active"` (quoted) for a string enum or `3.14` for a number enum.
	Literal string `json:"literal,omitzero"`
}

type GlobalType string

const (
	GlobalAPIKey    GlobalType = "APIKey"
	GlobalVersion   GlobalType = "Version"
	GlobalClient    GlobalType = "Client"
	GlobalUserAgent GlobalType = "User-Agent"
)

type Params []Param

func (ps Params) Required() bool {
	for _, p := range ps {
		if p.Required {
			return true
		}
	}

	return false
}

// Param represents a path or query parameter.
type Param struct {
	GlobalType   GlobalType `json:"globalType,omitzero"`
	VarName      string     `json:"varName,omitzero"`
	EnvName      string     `json:"envName,omitzero"`
	GoName       string     `json:"goName,omitzero"`
	FieldName    string     `json:"fieldName,omitzero"`
	JSONName     string     `json:"jsonName,omitzero"`
	Type         string     `json:"type,omitzero"`
	Required     bool       `json:"required,omitzero"`
	ParseExpr    string     `json:"parseExpr,omitzero"`
	ParseCast    string     `json:"parseCast,omitzero"`
	ParseErrFree bool       `json:"parseErrFree,omitzero"`
	IsEnum       bool       `json:"isEnum,omitzero"`
	// BaseType is the underlying Go type a generated Type was declared
	// from -- e.g. "string" for an enum's Type "Status", or for any other
	// named component built from a plain scalar. Set whenever Type came
	// from a $ref, since a generated name carries no zero-value or
	// formatting behavior of its own, unlike a builtin: NotZero and
	// FormatExpr switch on this instead, when set.
	BaseType string `json:"baseType,omitzero"`
	// IsUnixTime is true when Type is "time.Time" but the OpenAPI schema
	// itself is an integer (format: date-time), not a date-time string --
	// see [integerGoType]. FormatExpr needs this to know whether to encode
	// the param back into an integer instead of an RFC 3339 string.
	IsUnixTime  bool   `json:"isUnixTime,omitzero"`
	Description string `json:"description,omitzero"`
	Value       string `json:"value,omitzero"`   // hardcoded value, always the same
	Example     string `json:"example,omitzero"` // hardcoded example for tests
}

func (doc Document) APIKey() *Param {
	return doc.getGlobal(GlobalAPIKey)
}

func (doc Document) Client() *Param {
	return doc.getGlobal(GlobalClient)
}

func (doc Document) getGlobal(tp GlobalType) *Param {
	if i := slices.IndexFunc(doc.GlobalParams, func(p Param) bool {
		return p.GlobalType == tp
	}); i > -1 {
		p := doc.GlobalParams[i]
		return &p
	}

	return nil
}

// GoType is a resolved Go type reference.
type GoType struct {
	Name          string `json:"name,omitzero"`
	IsPointer     bool   `json:"isPointer,omitzero"`
	IsSlice       bool   `json:"isSlice,omitzero"`
	IsArrayOfSize int    `json:"isArrayOfSize,omitzero"`
	// IsNilable is true for a $ref to a named component schema that is
	// itself array-kind (e.g. "type TimeEntries []TimeEntry"): Name is
	// already a nilable Go type on its own, so Nilable returns it
	// unchanged instead of adding a pointer.
	IsNilable bool `json:"isNilable,omitzero"`
}

// String returns the Go type expression.
func (t GoType) String() string {
	switch {
	case t.IsPointer:
		return "*" + t.Name
	case t.IsSlice:
		return "[]" + t.Name
	case t.IsArrayOfSize > 0:
		return fmt.Sprintf("[%d]%s", t.IsArrayOfSize, t.Name)
	default:
		return t.Name
	}
}

func (t GoType) Nilable() string {
	switch {
	case t.IsSlice:
		return "[]" + t.Name
	case t.IsArrayOfSize > 0:
		return fmt.Sprintf("[%d]%s", t.IsArrayOfSize, t.Name)
	case t.IsNilable:
		return t.Name
	default:
		return "*" + t.Name
	}
}

// ZeroValue returns the Go zero-value literal for the type.
func (t GoType) ZeroValue() string {
	if t.IsPointer || t.IsSlice || t.IsArrayOfSize > 0 {
		return "nil"
	}

	switch t.Name {
	case "string":
		return `""`
	case "bool":
		return "false"
	case "int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64":
		return "0"
	case "uuid.UUID":
		return "uuid.Nil()"
	default:
		return t.Name + "{}"
	}
}

type Responses []Response

func (rs Responses) HasDefault() bool {
	return slices.ContainsFunc(rs, func(r Response) bool { return r.StatusCode == "default" })
}

// Response represents one expected HTTP response from an operation.
type Response struct {
	StatusCode  string  `json:"statusCode,omitzero"`
	GoConstant  string  `json:"goConstant,omitzero"`
	Description string  `json:"description,omitzero"`
	ContentType string  `json:"contentType,omitzero"`
	GoType      *GoType `json:"goType,omitempty"`
	IsSuccess   bool    `json:"isSuccess,omitzero"`
	// IsRawBytes is true when ContentType has no JSON media type declared for
	// it, so GoType is a raw []byte read directly from the response body
	// rather than something to json.Unmarshal into.
	IsRawBytes bool `json:"isRawBytes,omitzero"`
}

// ReqBody is the IR representation of an operation request body.
type ReqBody struct {
	TypeName    string `json:"typeName,omitzero"`
	ContentType string `json:"contentType,omitzero"`
	Required    bool   `json:"required,omitzero"`
}

type Auth struct {
	Bearer Bearer `json:"bearer,omitzero"`
	Basic  Basic  `json:"basic,omitzero"`
}

type Bearer struct {
	Name string `json:"name,omitzero"`
}

type Basic struct {
	UsernameEnvName string `json:"usernameEnvName,omitzero"`
	PasswordEnvName string `json:"passwordEnvName,omitzero"`
}

// BaseURLExpr returns the Go expression for the URL an operation builds its
// request path on: the client's, or serverURL with the server the path item
// named for itself. The latter stays overridable -- WithBaseURL has to reach
// every operation, or a caller cannot point the client at a test server.
func (op Operation) BaseURLExpr() string {
	if op.BaseURL == nil {
		return "c.baseURL"
	}

	return fmt.Sprintf("c.serverURL(&url.URL{Scheme: %q, Host: %q, Path: %q})",
		op.BaseURL.Scheme, op.BaseURL.Host, cmp.Or(op.BaseURL.Path, "/"))
}
