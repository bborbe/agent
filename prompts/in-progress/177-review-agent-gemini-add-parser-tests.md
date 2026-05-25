---
status: approved
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
---

<summary>
- Parser package has 0% test coverage despite being the core business logic
- All error paths in `Parse` are untested: empty input, API failure, JSON unmarshal failure
- `buildSchemaForTypeAtDepth` has 10 untested branches including recursive depth limit and all type kinds
- After this change, parser package achieves ≥80% statement coverage with all branches tested
</summary>

<objective>
Add comprehensive tests for the parser package covering all error paths and schema generation branches.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
- agent/gemini/pkg/parser/parser.go (full file, all functions)
- agent/gemini/pkg/parser/parser_test.go (existing test file if any)
- lib/mocks/agent-ai-parser.go (existing mock for AIParser interface)
</context>

<requirements>
1. **Create `agent/gemini/pkg/parser/parser_test.go`** if it doesn't exist

   Use Ginkgo/Gomega with external test package `parser_test`.

   Suite setup:
   ```go
   package parser_test

   import (
       "testing"
       "time"

       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
       "github.com/onsi/gomega/format"
   )

   func TestParser(t *testing.T) {
       time.Local = time.UTC
       format.TruncatedDiff = false
       RegisterFailHandler(Fail)
       RunSpecs(t, "Parser Suite")
   }
   ```

2. **Test `Parse` method** - add these cases:

   - Empty task content → returns error containing "task content is empty"
   - `buildGenAISchema` failure (pass nil target or invalid type) → returns wrapped error
   - `client.Models.GenerateContent` failure → returns wrapped error (use mock client)
   - `json.Unmarshal` failure (malformed JSON from mock) → returns wrapped error
   - Valid content → populates target struct correctly

   For testing with mock Gemini client, use `NewWithClient` constructor to inject a fake:
   ```go
   type fakeClient struct {
       generateContentErr error
       generateContentResult *genai.GenerateContentResult
   }
   // Implement required methods to satisfy the client interface
   ```

3. **Test `buildGenAISchema`** - add these cases:

   - nil target → returns empty object schema
   - `*Plan` struct → derives schema with operation, a, b fields
   - Pointer target → unwraps to underlying type

4. **Test `buildSchemaForTypeAtDepth`** - add these cases using a test struct:

   - `depth > maxSchemaDepth` (8) → returns error "exceeded max depth"
   - `string` type → returns `TypeString`
   - `int` type → returns `TypeInteger`
   - `bool` type → returns `TypeBoolean`
   - `float64` type → returns `TypeNumber`
   - `map[string]string` type → returns `TypeObject`
   - `[]string` type → returns `TypeArray` with `TypeString` items
   - Struct field with `json:"-"` tag → field is skipped
   - Struct field with `json:"name,omitzero"` → field name is "name"
   - Unknown kind (use custom type implementing interface differently) → returns error "unsupported kind"
   - Slice element error propagation

5. **Create test helper struct** for schema building tests:

   ```go
   type testPlan struct {
       Operation string `json:"operation"`
       A         int    `json:"a"`
       B         int    `json:"b"`
   }
   ```

6. **Run tests with coverage**
   ```bash
   cd agent/gemini && go test -v -coverprofile=/tmp/parser-cover.out -mod=vendor ./pkg/parser/...
   go tool cover -func=/tmp/parser-cover.out | grep "total:"
   ```

7. **Coverage must be ≥80%**

   If coverage is below 80%, add more test cases for uncovered branches.

8. **Run precommit**
   ```bash
   cd agent/gemini && make precommit
   ```
</requirements>

<constraints>
- Only change files in `agent/gemini/`
- Do NOT commit — dark-factory handles git
- Follow project conventions in `CLAUDE.md` and `docs/` — Ginkgo/Gomega, external test package
- Use `NewWithClient` to inject mock Gemini client for testing
- Coverage target: ≥80% statement coverage
- Error paths must be tested (if a function can fail, test the failure)
</constraints>

<verification>
cd agent/gemini && make precommit
</verification>
