package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/project"
)

func TestImportPetstore(t *testing.T) {
	dir := t.TempDir()
	res, err := Import("testdata/petstore.yaml", Options{OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != 4 || len(res.Files) != 2 || res.BaseURL != "https://petstore.example.com/v1" {
		t.Fatalf("%+v", res)
	}
	pets, err := os.ReadFile(filepath.Join(dir, "pets.http"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(pets)
	for _, want := range []string{
		"# @name list-pets", "GET {{baseUrl}}/pets\n", "    # ?limit={{limit}}  (optional)", "X-Trace: {{xTrace}}",
		"# @name create-pet", "# @assert status == 201", "# @description Creates a pet.", "Content-Type: application/json", `"name": "Rex"`, `"born": "{{$isoTimestamp}}"`,
		"# @name show-pet-by-id", "GET {{baseUrl}}/pets/{{petId}}",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("pets.http missing %q\n%s", want, s)
		}
	}
	api, _ := os.ReadFile(filepath.Join(dir, "api.http"))
	if !strings.Contains(string(api), "# @name get-health") {
		t.Errorf("api.http:\n%s", api)
	}
	env, _ := os.ReadFile(filepath.Join(dir, "http-client.env.json"))
	if !strings.Contains(string(env), `"baseUrl": "https://petstore.example.com/v1"`) {
		t.Errorf("env: %s", env)
	}

	// The generated files must parse cleanly and be runnable.
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.Validate() {
		if d.Severity == "error" {
			t.Errorf("validate: %s", d)
		}
	}
	if len(p.Requests()) != 4 {
		t.Fatalf("parsed %d requests", len(p.Requests()))
	}

	// Existing files are not overwritten without --force.
	res2, err := Import("testdata/petstore.yaml", Options{OutDir: dir})
	if err != nil || len(res2.Skipped) != 2 || len(res2.Files) != 0 {
		t.Fatalf("%+v %v", res2, err)
	}
}

func TestImportResolvesServerVariables(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(spec, []byte(`
openapi: 3.0.3
info:
  title: t
  version: 1.0.0
servers:
  - url: https://{region}.example.com/{base}
    variables:
      region:
        default: us
      base:
        default: api
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "200":
          description: ok
`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseURL != "https://us.example.com/api" {
		t.Fatalf("base url: %q", res.BaseURL)
	}
}

func TestImportUsesFirstNonEmptyServerURL(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(spec, []byte(`
openapi: 3.0.3
info:
  title: t
  version: 1.0.0
servers:
  - url: ""
  - url: https://api.example.com
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "200":
          description: ok
`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseURL != "https://api.example.com" {
		t.Fatalf("base url: %q", res.BaseURL)
	}
}

func TestImportRefsPathParamsAndJSONInput(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(spec, []byte(`{
  "openapi": "3.1.0",
  "info": {"title": "t", "version": "1"},
  "servers": [{"url": "https://api.example.com/v2/"}],
  "paths": {
    "/orgs/{orgId}/members": {
      "parameters": [{"$ref": "#/components/parameters/OrgId"}],
      "get": {
        "operationId": "listMembers",
        "tags": ["members"],
        "parameters": [{"name": "limit", "in": "query", "schema": {"type": "integer"}}],
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "array"}}}}}
      },
      "post": {
        "operationId": "addMember",
        "tags": ["members"],
        "requestBody": {"$ref": "#/components/requestBodies/Member"},
        "responses": {"201": {"description": "created"}}
      }
    }
  },
  "components": {
    "parameters": {"OrgId": {"name": "orgId", "in": "path", "required": true, "schema": {"type": "string"}}},
    "requestBodies": {"Member": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Member"}}}}},
    "schemas": {
      "Member": {
        "type": "object",
        "properties": {
          "email": {"type": "string", "format": "email"},
          "nickname": {"type": ["string", "null"]},
          "roles": {"type": "array", "items": {"$ref": "#/components/schemas/Role"}},
          "manager": {"$ref": "#/components/schemas/Member"}
        }
      },
      "Role": {"type": "string", "enum": ["admin", "member"]}
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseURL != "https://api.example.com/v2" || res.Requests != 2 {
		t.Fatalf("%+v", res)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "out", "members.http"))
	s := string(out)
	for _, want := range []string{
		"# @name list-members", "GET {{baseUrl}}/orgs/{{orgId}}/members\n", "    # ?limit={{limit}}  (optional)", "Accept: application/json",
		"# @name add-member", "# @assert status == 201", "POST {{baseUrl}}/orgs/{{orgId}}/members\n", "Content-Type: application/json",
		`"email": "user@example.com"`, `"nickname": "string"`, `"roles": [` + "\n    \"admin\"", `"manager": {`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("members.http missing %q\n%s", want, s)
		}
	}
	// Property order follows the schema.
	if strings.Index(s, `"email"`) > strings.Index(s, `"nickname"`) || strings.Index(s, `"nickname"`) > strings.Index(s, `"roles"`) {
		t.Errorf("property order not preserved:\n%s", s)
	}
}

func TestImportRejectsSwagger2(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte("swagger: '2.0'\ninfo: {title: t, version: '1'}\npaths: {}\n"), 0o644)
	if _, err := Import(spec, Options{OutDir: dir}); err == nil || !strings.Contains(err.Error(), "Swagger 2.0") {
		t.Fatalf("got %v", err)
	}
}

func TestImportExamplesAndBoolCase(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /things:
    post:
      operationId: makeThing
      parameters:
        - name: X-Req
          in: header
          required: True
          schema: {type: string}
      requestBody:
        content:
          application/json:
            examples:
              summaryOnly:
                summary: no value here
              real:
                value: {"name": "from-example"}
            schema: {type: object, properties: {name: {type: string}}}
      responses:
        "201": {description: created}
`), 0o644)
	if _, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "out", "api.http"))
	s := string(out)
	if !strings.Contains(s, `"name": "from-example"`) {
		t.Errorf("should use the first example with a value:\n%s", s)
	}
	if !strings.Contains(s, "\nX-Req: {{xReq}}\n") {
		t.Errorf("required: True should be treated as required:\n%s", s)
	}
}

func TestImportExplicitNullExample(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /reset:
    post:
      operationId: reset
      requestBody:
        content:
          application/json:
            example: null
      responses:
        "204": {description: done}
`), 0o644)
	if _, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "out", "api.http"))
	if !strings.Contains(string(out), "Content-Type: application/json\n\nnull\n") {
		t.Errorf("explicit null example should produce a null body:\n%s", out)
	}
}

func TestImportSchemaPrecedenceNullAndPointerIndex(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte(`
openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                pick: {type: string, default: from-default, examples: [from-examples]}
                nothing: {type: "null"}
                first: {$ref: "#/components/schemas/Envelope/allOf/0"}
      responses:
        "200": {description: ok}
  /b:
    post:
      operationId: b
      requestBody:
        content:
          application/json:
            schema: {type: "null"}
      responses:
        "200": {description: ok}
components:
  schemas:
    Envelope:
      allOf:
        - type: object
          properties: {id: {type: integer}}
`), 0o644)
	if _, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "out", "api.http"))
	s := string(out)
	for _, want := range []string{`"pick": "from-examples"`, `"nothing": null`, `"first": {` + "\n    \"id\": 1", "# @name b\n# @assert status == 200\nPOST {{baseUrl}}/b\nContent-Type: application/json\n\nnull\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestImportPatternedStatusAndRefSiblings(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte(`
openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /pets:
    post:
      operationId: createPet
      requestBody:
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/Pet"
              example: {"name": "inline-wins"}
      responses:
        "2XX": {description: any success}
components:
  schemas:
    Pet:
      type: object
      properties: {name: {type: string, example: from-ref}}
`), 0o644)
	if _, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "out", "api.http"))
	s := string(out)
	for _, want := range []string{"# @assert status >= 200\n# @assert status < 300\n", `"name": "inline-wins"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "2XX") {
		t.Errorf("patterned status must not be emitted literally:\n%s", s)
	}
}

func TestImportDisambiguatesCollidingParamNames(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /u/{user-id}:
    get:
      operationId: getU
      parameters:
        - {name: user-id, in: path, required: true, schema: {type: string}}
        - {name: user_id, in: query, required: true, schema: {type: string}}
        - {name: userId, in: header, required: true, schema: {type: string}}
      responses:
        "200": {description: ok}
`), 0o644)
	if _, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "out", "api.http"))
	s := string(out)
	for _, want := range []string{"GET {{baseUrl}}/u/{{userId}}\n", "?user_id={{userIdQuery}}", "userId: {{userIdHeader}}"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestImportEmptyServerDefaultAndCookies(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
servers:
  - url: https://api.example.com/{base}
    variables:
      base: {default: ""}
paths:
  /me:
    get:
      operationId: me
      parameters:
        - {name: session, in: cookie, required: true, schema: {type: string}}
        - {name: theme, in: cookie, schema: {type: string}}
        - {name: csrf, in: cookie, required: true, schema: {type: string}}
      responses:
        "200": {description: ok}
`), 0o644)
	res, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseURL != "https://api.example.com" {
		t.Fatalf("empty default should substitute: %q", res.BaseURL)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "out", "api.http"))
	s := string(out)
	for _, want := range []string{"Cookie: session={{session}}; csrf={{csrf}}\n", "# Cookie: theme={{theme}}  (optional)\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestImportTagFileCollisionsOptionalFirstQueryAndRefSiblings30(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
servers:
  - url: https://api/{region}
    variables:
      region: {}
paths:
  /a:
    get:
      operationId: a
      tags: ["foo/bar"]
      parameters:
        - {name: opt, in: query, schema: {type: string}}
        - {name: req, in: query, required: true, schema: {type: string}}
      responses: {"200": {description: ok}}
  /b:
    post:
      operationId: b
      tags: ["foo-bar"]
      requestBody:
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/Pet"
              example: {"name": "ignored-in-3.0"}
      responses: {"200": {description: ok}}
components:
  schemas:
    Pet: {type: object, properties: {name: {type: string, example: from-ref}}}
`), 0o644)
	if _, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")}); err == nil || !strings.Contains(err.Error(), "unresolved variable {region}") {
		t.Fatalf("a server variable without a default must stay unresolved: %v", err)
	}
	_ = os.WriteFile(spec, []byte(strings.Replace(mustRead(t, spec), "region: {}", "region: {default: eu}", 1)), 0o644)
	res, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 2 || !strings.HasSuffix(res.Files[0], "foo-bar-2.http") && !strings.HasSuffix(res.Files[1], "foo-bar-2.http") {
		t.Fatalf("colliding tags must get distinct files: %v", res.Files)
	}
	all := ""
	for _, f := range res.Files {
		all += mustRead(t, f)
	}
	if !strings.Contains(all, "    ?req={{req}}\n    # &opt={{opt}}  (optional)\n") {
		t.Errorf("active query parameters must precede optional comments:\n%s", all)
	}
	for _, f := range res.Files {
		if _, diags, err := httpfile.ParseFile(f); err != nil || len(diags) > 0 {
			t.Errorf("generated file must parse: %v %v", err, diags)
		}
	}
	if !strings.Contains(all, `"name": "from-ref"`) || strings.Contains(all, "ignored-in-3.0") {
		t.Errorf("3.0 must ignore keys next to $ref:\n%s", all)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestImportCyclesAndLiteralRefInExamples(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	// A recursive anchor inside an example must not recurse forever.
	_ = os.WriteFile(spec, []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          application/json:
            example: &loop {"self": *loop, "$ref": "#/components/schemas/Pet", "name": "literal"}
      responses: {"200": {description: ok}}
components:
  schemas:
    Pet: {type: object, properties: {name: {type: string, example: from-schema}}}
`), 0o644)
	if _, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	out := mustRead(t, filepath.Join(dir, "out", "api.http"))
	if !strings.Contains(out, `"$ref": "#/components/schemas/Pet"`) || !strings.Contains(out, `"name": "literal"`) || strings.Contains(out, "from-schema") {
		t.Errorf("example data must be kept literal, including a $ref key:\n%s", out)
	}
	// A cyclic $ref chain is an error, not a silently empty schema.
	_ = os.WriteFile(spec, []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /b:
    post:
      operationId: b
      requestBody:
        content:
          application/json:
            schema: {$ref: "#/components/schemas/A"}
      responses: {"200": {description: ok}}
components:
  schemas:
    A: {$ref: "#/components/schemas/B"}
    B: {$ref: "#/components/schemas/A"}
`), 0o644)
	if _, err := Import(spec, Options{OutDir: filepath.Join(dir, "out2")}); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("cyclic $ref should be reported: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "out2")); len(entries) != 0 {
		t.Fatalf("no files may be written when the document is broken: %v", entries)
	}
}

func TestImportOptionalOnlyQuerySeparators(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(spec, []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
servers: [{url: https://api}]
paths:
  /a:
    get:
      operationId: a
      parameters:
        - {name: first, in: query, schema: {type: string}}
        - {name: second, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
`), 0o644)
	res, err := Import(spec, Options{OutDir: filepath.Join(dir, "out")})
	if err != nil {
		t.Fatal(err)
	}
	all := mustRead(t, res.Files[0])
	if !strings.Contains(all, "    # ?first={{first}}  (optional)\n    # &second={{second}}  (optional)\n") {
		t.Errorf("only the first optional parameter may use ?:\n%s", all)
	}
}
