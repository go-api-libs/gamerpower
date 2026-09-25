# MarkRosemaker/openapi-codegen

NOTE: This plan is partly outdated.

## Context

`faetools/apilib` is the existing OpenAPI code generator that produces Go client libraries in the `go-api-libs/*` ecosystem. It works but has accumulated significant tech debt: tangled responsibilities, low test coverage (~5%), and third-party dependency sprawl. Rather than continuing to refactor it, we're starting a clean greenfield project — `github.com/MarkRosemaker/openapi-codegen` — that replaces the code generation capability with a focused, well-tested, minimal-dependency tool.

**Goal:** Parse an OpenAPI 3.x spec, flatten it, and generate idiomatic Go code — types, HTTP client, HTTP server, and tests. The generated output must be compatible with the existing `go-api-libs/*` ecosystem (same patterns, same runtime deps: `go-api-libs/api`, `go-api-libs/types`, `MarkRosemaker/jsonutil`).

**Working style:** small iterative steps, ≥90% test coverage on new code, one PR per logical change.

**Project location:** `/home/user/openapi-codegen`. GitHub repo: `MarkRosemaker/openapi-codegen`.

---

## Design Decisions

### Code generation: `text/template` (not `dave/jennifer`)

- Templates are self-documenting — reading a `.go.tmpl` shows exactly what the output looks like.
- Zero external dependency (stdlib `text/template`).
- oapi-codegen proves `text/template` works at scale for this exact problem.

Templates embedded via `//go:embed`.

### Import management: goimports + gofumpt post-processing

Templates include all potentially needed imports. After rendering, `golang.org/x/tools/imports` (goimports) removes unused imports and adds missing ones, then `gofumpt` formats.

### Dependency budget

**Direct deps (owned or quasi-stdlib):**
| Module | Purpose | Owner |
|---|---|---|
| `github.com/MarkRosemaker/openapi` | OpenAPI 3.x model, parsing, validation | owned |
| `github.com/MarkRosemaker/openapi-flatten` | Flatten inline schemas → named components | owned |
| `github.com/ettle/strcase` | Case conversion (PascalCase, camelCase, SNAKE) | third-party, small, zero transitive deps |
| `golang.org/x/tools/imports` | goimports for post-processing generated code | quasi-stdlib |
| `mvdan.cc/gofumpt` | Strict Go formatting | quasi-stdlib |

**Runtime deps of generated code:**
| Module | Purpose |
|---|---|
| `encoding/json/v2` | JSON with `omitzero` (Go 1.26+ stdlib) |
| `github.com/go-api-libs/api` | Error types (`ErrUnknownStatusCode`, `WrapDecodingError`) |
| `github.com/go-api-libs/types` | `types.Email` etc. |
| `github.com/MarkRosemaker/jsonutil` | Custom JSON marshalers for `url.URL`, `time.Duration` |
| `github.com/google/uuid` | `uuid.UUID` (only if spec uses UUID format) |
| `cloud.google.com/go/civil` | `civil.Date` (only if spec uses date format) |

---

## Package Structure

```
github.com/MarkRosemaker/openapi-codegen/
├── cmd/openapi-codegen/
│   └── main.go                 # CLI entry point (flag-based)
├── generate.go                 # func Generate(cfg) error — top-level pipeline
├── config.go                   # type Config struct
├── ir/
│   ├── ir.go                   # IR type definitions (Document, Operation, Schema, etc.)
│   ├── document.go             # openapi.Document → ir.Document
│   ├── document_test.go
│   ├── schema.go               # Schema → GoType mapping
│   ├── schema_test.go
│   ├── operation.go            # Operation resolution (params, responses)
│   ├── operation_test.go
│   └── param.go                # Parameter resolution helpers
├── render/
│   ├── render.go               # Template loading, execution, formatting
│   ├── render_test.go
│   ├── funcs.go                # Template helper functions
│   ├── funcs_test.go
│   └── templates/
│       ├── types.go.tmpl
│       ├── client.go.tmpl
│       ├── server.go.tmpl
│       └── test.go.tmpl
├── testdata/
│   ├── freepublicapis/
│   │   ├── openapi.json
│   │   └── golden/
│   └── petstore/
├── go.mod
└── README.md
```

**Dependency graph:** `cmd/` → root → `render` → `ir`. The `ir` package has zero internal deps (only `MarkRosemaker/openapi`). The `render` package depends on `ir` for types but not on the OpenAPI model.

---

## Intermediate Representation (IR)

```go
type Document struct {
    PackageName    string
    BaseURL        URLParts
    UserAgent      string
    Operations     []Operation
    Schemas        []Schema
    HasURLFields      bool
    HasDurationFields bool
    HasDateFields     bool
}

type Operation struct {
    Name           string       // PascalCase operation ID
    Summary        string
    Method         string       // "GET", "POST", etc.
    PathTemplate   string       // "/apis/{id}"
    JoinPathArgs   []string     // pre-computed: `"apis"`, `strconv.Itoa(id)`
    PathParams     []Param
    QueryParams    []Param
    HasParams      bool
    ParamStructName string
    RequestBody    *RequestBody
    Responses      []Response
    SuccessReturn  *GoType
    Deprecated     bool
}

type Schema struct {
    Name        string
    Description string
    Kind        SchemaKind     // Struct, Enum, ArrayAlias
    Type        string
    Fields      []Field
    EnumValues  []EnumValue
}

type Field struct {
    Name     string   // Go PascalCase
    JSONName string   // original key
    Type     string   // Go type string
    JSONTag  string   // pre-computed: `json:"name,omitzero"`
    Description string
    Required bool
}

type Param struct {
    GoName     string
    JSONName   string
    Type       string
    Required   bool
    NotZero  string
    FormatExpr string
    IsEnum     bool
    Description string
}

type GoType struct {
    Name      string
    IsPointer bool
    IsSlice   bool
}

type Response struct {
    StatusCode  string
    GoConstant  string
    Description string
    ContentType string
    GoType      *GoType
    IsSuccess   bool
}
```

### Schema → Go type mapping:

```
boolean                  → bool
integer                  → int
integer + int64          → int64
integer + duration       → time.Duration
string                   → string
string + uuid            → uuid.UUID
string + uri             → url.URL
string + email           → types.Email
string + date-time       → time.Time
string + date            → civil.Date
string + ipv4/ipv6       → net.IP
number                   → float64
number + float           → float32
number + double          → float64
array                    → []ItemType
object (named)           → StructName
object (additionalProps) → map[string]ValueType
```

---

## Pipeline

```
openapi.json/yaml → openapi.LoadFromFile() → flatten.Document() → ir.FromDocument(doc, cfg) → render.Files(irDoc, cfg) → write to output
```

---

## Implementation Order

| Step | What | Key files |
|---|---|---|
| **1** | README + module init | `README.md`, `go.mod` |
| **2** | IR types + schema→type mapping | `ir/ir.go`, `ir/schema.go`, `ir/schema_test.go` |
| **3** | Schema resolution (struct fields, enums, aliases) | `ir/schema.go` (expand), `ir/schema_test.go` |
| **4** | Operation resolution (params, responses, path building) | `ir/operation.go`, `ir/operation_test.go` |
| **5** | Full document→IR conversion | `ir/document.go`, `ir/document_test.go` |
| **6** | Render engine + types template | `render/render.go`, `render/funcs.go`, `templates/types.go.tmpl`, golden tests |
| **7** | Client template | `templates/client.go.tmpl`, golden tests |
| **8** | Test template | `templates/test.go.tmpl`, golden tests |
| **9** | Server template | `templates/server.go.tmpl`, golden tests |
| **10** | CLI + end-to-end | `cmd/openapi-codegen/main.go`, `generate.go`, integration test |
| **11** | Additional fixtures (petstore, jobicy) | `testdata/` expansions |

---

## Verification

After every step: `go build ./...`, `go vet ./...`, `go test ./... -coverprofile=coverage.out` — ≥90% on touched packages.

---

## REFERENCE: Existing apilib Logic to Port

### Schema Type Mapping (`internal/gen/schema.go`)
- `newSchema()` recursively parses OpenAPI `SchemaRef` to build nested type structures
- Supports **AllOf** composition: merges properties from multiple schemas into single struct
- **Pointer heuristic**: Fields default to pointers unless marked required or are plain strings. Arrays always pointer.
- String format panics on unknown format (strict validation)
- Enum name sanitization: numeric prefixes use `num2words`, special chars replaced (`#`→Sharp, `/`→space)

### Field Naming & JSON Tags (`internal/gen/field.go`)
- `Name()`: Sanitize JSON name → PascalCase via `strcase.ToGoPascal` after text replacement (`+`→" Plus ", `.`→" Dot ", `/`→space, parens stripped, `C#`→`C-Sharp`)
- Leading digits converted via `num2words` (e.g., "4K" → "Four K")
- `jsonTags()`: `omitempty` for non-required fields (except plain strings which skip it), `omitzero` for required int/bool/formatted-string fields
- Arrays always get `omitempty=true`

### Operations (`internal/gen/operation.go`)
- Merges path + operation parameters, validates uniqueness
- Path params: parsed via `analyze.ParsePath` → `baseURL.JoinPath(segments...)` with type conversion (UUID→`.String()`, int→`strconv.Itoa()`)
- Query params: build `url.Values` map with zero-check conditionals; arrays with `explode=true` pass whole slice, otherwise join with `,`
- Response deduplication: same response structure across multiple status codes merged
- Request body uses `io.Pipe()` + goroutine for streaming JSON marshal

### Client Generation (`internal/gen/client.go`)
- Global const `userAgent`, global var `baseURL` from first server URL, global var `jsonOpts` combining marshalers
- JSON marshaler/unmarshaler injection for special formats (URLs, Duration as int seconds, Date as int unix, DateTime as int unix)
- Generic type overloads for operations with custom response/request bodies
- Auth: basic (username+password), bearer (env var), or none

### Types Generation (`internal/gen/types.go`)
- Only generates schemas **referenced** by operations (transitive walk via `walkSchema`)
- Query param structs: one field per query param, PascalCased, with enum types pre-declared
- Object → struct, string+enum → type alias + const block, array → slice, additionalProperties → `map[string]ValueType`

### Test Generation (`internal/gen/tests.go`)
- `testRoundTripper` mock hijacks transport with pre-set response
- Tests: error handling, response parsing with wrong status/content-type, VCR cassette replay
- VCR: reads YAML cassettes from `vcr/` directory, generates per-interaction tests

### Test Fixtures (`internal/gen/fixtures/`)
- **freepublicapis**: openapi.json + golden `.tpl` files (client, types, test)
- **jobicy**: openapi.json + golden `.tpl` files
- **remote-ok-jobs**: openapi.json + golden `.tpl` files

The golden `.tpl` files in apilib are the expected output — use them as reference for what the templates should produce.

---

**Instruction:** Implement this plan starting from Step 1. The reference codebase is at `/home/user/apilib` if you need to inspect specific implementation details. Start with `go mod init github.com/MarkRosemaker/openapi-codegen` and a minimal README, then proceed to Step 2.
