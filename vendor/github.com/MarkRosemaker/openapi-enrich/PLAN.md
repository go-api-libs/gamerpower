# MarkRosemaker/openapi-enrich

## Context

`faetools/apilib` is an existing monolithic tool that records HTTP interactions, analyzes
them, and updates OpenAPI specs. We're decomposing it into focused libraries:

1. **openapi-codegen** (in progress) — generates Go code from an OpenAPI spec
2. **openapi-enrich** (this project) — enriches an OpenAPI spec from observed HTTP traffic
3. (future) A probe/recorder tool that collects HTTP interactions

This project is #2: a focused library with a single public function that takes an
OpenAPI document and a slice of HTTP interactions, then enriches the spec in place —
adding paths, operations, parameters, schemas, and responses inferred from the traffic.

The caller handles everything else: parsing input files, loading/saving the spec,
flattening, tidying, sorting — any post-processing is the caller's responsibility.

**Working style:** small iterative steps, ≥90% test coverage on new code, one PR per
logical change.

**Project location:** `/home/user/openapi-enrich`. GitHub repo: `MarkRosemaker/openapi-enrich`.

---

## Design Decisions

### Minimal public API

```go
// Enrich updates doc in place based on observed HTTP interactions.
// It adds paths, operations, parameters, request bodies, and response schemas
// inferred from the interactions. Schemas are left inline — the caller can
// flatten (openapi-flatten), tidy (operation IDs, security hoisting), or
// sort component maps afterward as desired.
func Enrich(doc *openapi.Document, interactions Interactions) error
```

Plus a helper:

```go
// NewDocument creates a minimal valid OpenAPI 3.1.0 document as a starting point.
func NewDocument() *openapi.Document
```

That's the entire public surface. No flatten, no tidy, no sorting — those are
separate concerns the caller composes as needed.

### Own interaction types (no external dependency)

```go
type Interaction struct {
    Request  Request
    Response Response
}

type Request struct {
    Method  string
    URL     string
    Headers http.Header
    Body    []byte
}

type Response struct {
    StatusCode int
    Headers    http.Header
    Body       []byte
}
```

The caller constructs these from whatever source (HAR, VCR, raw HTTP, custom).

### Dependency budget

| Module | Purpose | Owner |
|---|---|---|
| `github.com/MarkRosemaker/openapi` | OpenAPI 3.x model | owned |
| `github.com/MarkRosemaker/openapi-merge` | Merge schemas, parameters, responses | owned |
| `github.com/ettle/strcase` | Case conversion for operation IDs | third-party, small |

Three deps. No flatten (caller's choice), no CLI deps, no file I/O deps.

---

## Package Structure

```
github.com/MarkRosemaker/openapi-enrich/
├── enrich.go                      # func Enrich(doc, interactions) error
├── interaction.go                 # type Interaction, Request, Response
├── document.go                    # func NewDocument()
├── analyze.go                     # Process single interaction → update doc
├── analyze_test.go
├── schema.go                      # JSON v2 value → openapi.Schema inference
├── schema_test.go
├── path.go                        # Path matching, parameter detection
├── path_test.go
├── response.go                    # Response analysis (status, content-type, body)
├── response_test.go
├── operation_id.go                # OperationID inference from method+path
├── operation_id_test.go
├── testdata/
│   ├── freepublicapis/ # DONE
│   │   ├── interactions.json      # Interactions fixture
│   │   └── golden.json            # Expected output spec
│   └── petstore/ # DONE
│       └── ...
├── go.mod
└── README.md
```

Single package — `package enrich`. Internal helpers unexported.

---

## What `Enrich()` does

```go
func Enrich(doc *openapi.Document, interactions Interactions) error {
    for _, ia := range interactions {
        if err := analyzeInteraction(doc, &ia); err != nil {
            return fmt.Errorf("%s %s: %w", ia.Request.Method, ia.Request.URL, err)
        }
    }
    return nil
}
```

That's it — iterate interactions, update doc. No flatten, no tidy, no sorting.

---

## What `analyzeInteraction()` does (ported from [apilib](https://github.com/faetools/apilib))

For each interaction:
1. **Server setup**: Initialize `doc.Servers[0]` from first request URL if empty
2. **Skip different hosts**: Only process interactions matching the server base URL
3. **Find/create path item** and **find/create operation** for the HTTP method
4. **Query parameters**: Parse from URL, infer schema from values, detect comma-separated
   as non-exploded arrays, merge with existing params
5. **Request headers**:
   - `Authorization` → create SecurityScheme (Basic/Bearer) + SecurityRequirement
   - `Content-Type` → track for request body analysis
   - Custom headers → header parameters
   - Ignore: `User-Agent`, `Referer`, `Cookie`
6. **Request body**: If JSON, infer schema via `newSchemaFromJSON()`, merge with existing
7. **Response**: Infer schema from JSON body, merge with existing for same status code
8. **Operation ID**: Infer from method+path (e.g. `GET /users` → `ListUsers`)

### Schema inference (`schema.go`)

`newSchemaFromJSON(data []byte) (*openapi.Schema, error)` — token-based JSON v2 decoder:

- **Strings**: detect format — UUID, URI, Email, DateTime (RFC3339), IPv4/IPv6
- **Numbers**: `strconv.Atoi` succeeds → integer, else → number
- **Booleans**: direct
- **null**: treated as object placeholder (merge promotes when real type seen)
- **Objects**: recurse on each property, mark all as required
- **Arrays**: merge item schemas across elements; empty → placeholder object items

### Path matching (`path.go`)

`findPathItem(doc, requestURL)`:
1. Compute relative path by stripping server base URL
2. Exact match in `doc.Paths`
3. Parametric match (segment count + type compatibility)
4. No match → create new path entry

### Operation ID inference (`operation_id.go`)

- Normalize existing to PascalCase
- Or infer: `GET /users` → `ListUsers`, `GET /users/{id}` → `GetUserByID`,
  `POST /items` → `PostItems`

---

## Testing Strategy

### Golden-file tests
1. Start from empty spec + interaction fixtures
2. Run `Enrich(doc, interactions)`
3. Compare output spec against golden JSON

### Unit tests (table-driven)
- `schema_test.go`: every JSON value type/format → schema
- `path_test.go`: path matching, parameter extraction
- `response_test.go`: response schema inference
- `operation_id_test.go`: method+path → operation ID

### Coverage target: ≥90%

---

## Implementation Order

| Step | What | Key files |
|---|---|---|
| **1** | Module init + types | `go.mod`, `interaction.go`, `document.go`, `README.md` |
| **2** | Schema inference from JSON | `schema.go`, `schema_test.go` |
| **3** | Path matching & parameter detection | `path.go`, `path_test.go` |
| **4** | Response analysis | `response.go`, `response_test.go` |
| **5** | Operation ID inference | `operation_id.go`, `operation_id_test.go` |
| **6** | Full interaction analysis | `analyze.go`, `analyze_test.go` |
| **7** | Top-level Enrich + golden tests | `enrich.go`, `testdata/`, golden tests |
| **8** | Additional fixtures | More test APIs |

---

## Key gotchas from existing [apilib](https://github.com/faetools/apilib)

1. **null → object**: null JSON defaults to object schema; merge promotes when real type seen
2. **Comma-separated query params**: `?tags=a,b,c` → non-exploded array, `explode: false`
3. **Required fields**: all properties found in a JSON object marked required initially;
   merge relaxes if later interactions omit a field
4. **Empty arrays**: get placeholder object items, refined on non-empty array
5. **AllOf consistency**: merge enforces same type across allOf entries
6. **Schema merging null**: if one schema from null, adopt the other's type/format entirely
7. **Path ordering**: preserve insertion order (order of discovery)

---

## REFERENCE: Existing [apilib](https://github.com/faetools/apilib) Source Code

### analyze/interaction.go — Full Flow (353 lines)

For each interaction:
1. Parse request URL, init `doc.Servers[0]` if empty (uses `path.Dir()` of URL as base)
2. Skip if `reqURL.Host != defaultURL.Host`
3. `findPathItem()` returns existing or nil. If nil, delete "/" placeholder, create new PathItem
4. Get/create Operation for HTTP method via `pi.SetOperation()`
5. **Query params** (lines 118-184): each param:
   - Contains `,` → build JSON array, infer schema, set `explode: false`
   - Otherwise → wrap as JSON string (or number if `Atoi` succeeds), infer schema
   - Check path-level then op-level params — merge if found, append if new
6. **Headers** (lines 186-260):
   - `Authorization`: parse Basic/Bearer, create SecurityScheme + SecurityRequirement
   - `Content-Type`: save media range
   - Custom headers: create required header parameter
   - Ignored: User-Agent, Referer, Cookie
   - Unknown → error
7. **Request body** (lines 262-302): JSON → infer schema, create/merge RequestBody
8. **Operation ID** (line 305): infer from method+path
9. **Response** (lines 309-328): `newResponse()`, merge if status code exists

### analyze/schema.go — newSchemaFromJSON

Token-based JSON decoder:
- `'"'` → string + `stringFormat()` (UUID, URI, HTML check, Email, DateTime, IPv4/6)
- `'0'` → `numFormat()` (`Atoi` → integer, else number)
- `'t'/'f'` → boolean
- `'n'` → object placeholder
- `'{'` → recurse properties, all required
- `'['` → merge item schemas, empty → placeholder

`stringFormat()` tries in order: UUID (`uuid.Parse`), URI (has scheme+host),
HTML (reject if non-text nodes), Email (`types.Email.Validate`), DateTime (RFC3339),
IPv4/6 (`net.ParseIP`).

### analyze/path.go — Path Matching

`ParsedPath` = `[]*PathElement` (static name or parameter).

`findPathItem()`: exact match → parametric match → create new.
`Fits()` checks segment count + type (integers must parse).

### analyze/response.go — Response Analysis

`newResponse()`: parse Content-Type via `mime.ParseMediaType`, JSON → schema,
text/plain → string if body differs from status text, text/html → example only.
Ignores ~50+ infrastructure headers (CDN, cache, security). Unknown → error.

### tidy/operation.go — OperationID (for reference, caller may use separately)

`pathToName()`: split on `/`, `{id}` after segment → singularize + `by_ID`.
`GET /users` → `ListUsers`, `POST /items` → `PostItems`, `GET /users/{id}` → `GetUserByID`.

### tidy/security.go — Security hoisting (for reference, caller may use separately)

Collect all security requirements. If one appears on ALL operations, move to doc level.

### openapi-merge — Schema merging

`Schema(a, b, isParam)`: merge title/description, handle allOf, null promotion,
examples, properties, enums. Also: `Parameter()`, `Response()`, `Content()`, `MediaType()`.

### openapi-flatten — For reference (caller's responsibility)

`Document()`: moves inline objects-with-properties and strings-with-enums to
`#/components/schemas/`, creates `$ref` pointers. Context-aware naming with collision
resolution. **Not needed for enrichment correctness** — purely organizational.

---

**Instruction:** Implement this plan starting from Step 1. Reference codebase at
https://github.com/faetools/apilib — particularly `internal/analyze/`, `internal/tidy/`, vendor package
`openapi-merge`. Start with `go mod init github.com/MarkRosemaker/openapi-enrich`,
the Interaction type, NewDocument(), and a minimal README, then proceed to Step 2.
