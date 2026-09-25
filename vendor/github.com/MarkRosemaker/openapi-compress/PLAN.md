# Plan: Implement Schema Deduplication in `Document()`

## Context

The `openapi-compress` library compresses OpenAPI specs. The main entry point `Document(d *openapi.Document) error` in `document.go` is currently a stub (`return nil`). The goal of this first iteration is to find schemas in `components.schemas` that are structurally identical and merge them: keep one canonical name, remove the duplicates, and replace all `$ref` pointers throughout the document.

Golden files (used by tests) are regenerated via `go generate` whenever the implementation changes.

## Branch Setup

- Pull latest `main`, create/checkout branch

## Key Types (in `vendor/github.com/MarkRosemaker/openapi/`)

- `Schema` — all fields in `schema.go`: Title, Description, Type, Format, AllOf (SchemaRefList), Min/Max (*float64), Pattern (*regexp.Regexp), Enum ([]string), MinItems/MaxItems, Items (*SchemaRef), Properties (SchemaRefs), Required ([]string), AdditionalProperties (*SchemaRef), ContentMediaType, ContentEncoding, Default (any), Example (jsontext.Value), Extensions
- `SchemaRef = refOrValue[Schema, *Schema]` — either holds `Ref *Reference` (with `Identifier` like `#/components/schemas/Name`) or `Value *Schema` (inline). After loading, if Ref is set, Value is also resolved.
- `Schemas = map[string]*Schema` — the `components.schemas` map, with index-based ordering via `ordmap`

## Files to Create/Modify

1. **`document.go`** — implement `Document()` (main dedup logic + ref replacement walker)
2. **`schema_equal.go`** — implement `schemasEqual(a, b *openapi.Schema) bool` (structural equality)
3. **`testdata/*/golden.json`** — regenerated via `go generate ./...` after implementation

## Implementation Plan

### 1. `schema_equal.go` — Structural Equality

```go
// schemasEqual returns true if two schemas are structurally identical.
func schemasEqual(a, b *openapi.Schema) bool
```

Field-by-field comparison:
- Scalars (Title, Description, Type, Format, MinItems, ContentMediaType, ContentEncoding): `==`
- `*float64` (Min, Max), `*uint` (MaxItems): nil-safe pointer comparison
- `*regexp.Regexp` (Pattern): compare `.String()` values
- `[]string` (Enum, Required): `slices.Equal`
- `jsontext.Value` (Example): `bytes.Equal`
- `any` (Default): `reflect.DeepEqual`
- `Extensions`: compare as map (reflect.DeepEqual)
- `AllOf SchemaRefList`: compare length then element-by-element with `schemaRefEqual`
- `Items *SchemaRef`, `AdditionalProperties *SchemaRef`: nil-safe `schemaRefEqual`
- `Properties SchemaRefs`: compare key sets and values with `schemaRefEqual`

`schemaRefEqual(a, b *openapi.SchemaRef) bool`:
- Both nil → true
- One nil → false
- Both have `Ref != nil`: compare `Ref.Identifier`, `Ref.Summary`, `Ref.Description`
- Both have only `Value`: recurse `schemasEqual(a.Value, b.Value)`
- Mixed (one Ref, one inline): false

### 2. `document.go` — Dedup Logic

```
func Document(d *openapi.Document) error:
  1. Collect sorted schema names (deterministic order)
  2. O(n²) pairwise: for each pair (nameA < nameB), if schemasEqual → replacements[nameB] = nameA
     - Skip nameB if it's already in replacements (already assigned to a canonical)
     - NOTE: We could look into reducing the O(n²) complexity by putting schemas into buckets early (via hash maps maybe), where schemas in one bucket can't possibly be the same as schemas in another bucket.
  3. If no replacements found, return nil
  4. Delete replaced schemas from d.Components.Schemas
  5. Call walkSchemaRefs(d, replaceFunc) to update all $ref Identifiers
  6. Return nil
```

Canonical name = the lexicographically smallest name in a duplicate group (naturally emerges from sorted iteration: nameA always comes before nameB).

`replaceFunc`: given a `*SchemaRef`, if `ref.Ref != nil` and the referenced schema name is in `replacements`, update `ref.Ref.Identifier` to `"#/components/schemas/" + canonical`.

Helper `schemaNameFromRef(identifier string) string`: strips `#/components/schemas/` prefix.

### 3. `document.go` — Document Walker

`walkSchemaRefs(d *openapi.Document, fn func(*openapi.SchemaRef))` visits every `*SchemaRef` in the document:

- `d.Components.Schemas` → each `*Schema` via `walkSchema`
- `d.Components.Responses` → `Response.Content` → `MediaType.Schema`
- `d.Components.Parameters` → `Parameter.Content` → `MediaType.Schema` (note: `Parameter.Schema` is `*Schema`, not `*SchemaRef`, so it can't hold a `$ref`)
- `d.Components.RequestBodies` → `RequestBody.Content` → `MediaType.Schema`
- `d.Components.Headers` → `Header.Content` → `MediaType.Schema`
- `d.Components.PathItems` → each PathItem via `walkPathItem`
- `d.Paths` → each PathItem via `walkPathItem`
- `d.Webhooks` → each PathItemRef → PathItem via `walkPathItem`

`walkPathItem(p *PathItem, ...)`: walks `p.Parameters` (content schemas), then all operations (Get/Put/Post/Delete/Options/Head/Patch/Trace) via `walkOperation`.

`walkOperation(o *Operation, ...)`: walks `o.Parameters` (content schemas), `o.RequestBody` → content → schema, `o.Responses` → content → schema, `o.Callbacks` → PathItemRefs → PathItems.

`walkSchema(s *Schema, visited map[*Schema]bool, fn)`: recurse into `Properties`, `Items`, `AdditionalProperties`, `AllOf` (each is a `*SchemaRef`).

`walkSchemaRef(ref *SchemaRef, visited, fn)`: call `fn(ref)`, then recurse `walkSchema(ref.Value, ...)`. Uses `visited` set on `*Schema` pointers to prevent infinite recursion.

`walkContent(c Content, visited, fn)`: iterate media types, call `fn(mt.Schema)` if non-nil, then `walkSchemaRef`.

`walkParameterRef(p *ParameterRef, visited, fn)`: walk `p.Value.Content` only (not `p.Value.Schema` since that's `*Schema`).

### 4. Regenerate Golden Files and Run Tests

```bash
# After implementation:
go generate ./...      # regenerates testdata/*/golden.json
go test ./...          # should pass (golden files now match implementation)
```

Expected behavior on petstore test case: `GetV1PetByPetIDOkJSONResponseMedicalInfo` and `ListV1PetsOkJSONResponseDataItemsMedicalInfo` are identical → merge to `GetV1PetByPetIDOkJSONResponseMedicalInfo` (lexicographically smaller), and the reference `#/components/schemas/ListV1PetsOkJSONResponseDataItemsMedicalInfo` in the `medicalInfo` property is updated.

## Verification

1. `go build ./...` — compiles without errors
2. `go generate ./...` — golden files updated
3. `go test ./...` — all tests pass
4. Inspect `testdata/petstore/golden.json` diff to confirm the dedup happened correctly