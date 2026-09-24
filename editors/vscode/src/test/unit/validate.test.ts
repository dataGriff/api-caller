// Runs under plain node (npm run test:unit): nothing here imports vscode.
import * as assert from "node:assert";
import * as fs from "node:fs";
import * as path from "node:path";
import { byteToCharColumn, directivesFromGrammar, levenshtein, nearestDirective, parseStderrError, parseUsageError, parseValidateOutput, problemsByPath, toProblem, LINE_END } from "../../validate";

const fixture = JSON.stringify({
  ok: false,
  files: 2,
  requests: 5,
  diagnostics: [
    { path: "users.http", line: 12, column: 11, end_line: 12, end_column: 16, severity: "error", code: "unknown-selector", message: 'assert "bogus == 1": unknown selector "bogus"' },
    { path: "users.http", line: 3, column: 3, end_line: 3, end_column: 14, severity: "warning", code: "unknown-directive", message: "unknown directive @frobnicate (ignored)" },
    { path: "apic.yaml", line: 0, severity: "error", message: "auth.default: bad spec" },
    { path: "old.http", line: 7, severity: "error", message: "a finding from a release without spans" },
  ],
});

suite("validate output", () => {
  test("maps spans to 0-based, end-exclusive editor ranges", () => {
    const out = parseValidateOutput(fixture);
    assert.ok(out);
    const byPath = problemsByPath(out);
    const users = byPath.get("users.http");
    assert.ok(users);
    assert.strictEqual(users.length, 2);
    assert.deepStrictEqual(
      { ...users[0] },
      { line: 11, startColumn: 10, endLine: 11, endColumn: 15, wholeLine: false, severity: "error", code: "unknown-selector", message: 'assert "bogus == 1": unknown selector "bogus"' },
    );
    assert.strictEqual(users[1].startColumn, 2);
    assert.strictEqual(users[1].endColumn, 13);
  });

  test("a finding without a span covers the whole line, and line 0 the first line", () => {
    const byPath = problemsByPath(parseValidateOutput(fixture)!);
    const yaml = byPath.get("apic.yaml")![0];
    assert.strictEqual(yaml.line, 0);
    assert.strictEqual(yaml.wholeLine, true);
    assert.strictEqual(yaml.endColumn, LINE_END);
    const old = byPath.get("old.http")![0];
    assert.strictEqual(old.line, 6);
    assert.strictEqual(old.wholeLine, true);
  });

  test("byte columns become character columns when the line's text is known", () => {
    assert.strictEqual(byteToCharColumn(undefined, 9), 8);
    assert.strictEqual(byteToCharColumn("# @name ascii", 9), 8);
    // `é` is two bytes: a span after it starts one byte later than its character.
    assert.strictEqual(byteToCharColumn("# @name café", 9), 8);
    assert.strictEqual(byteToCharColumn("# @name café", 14), 12); // end, just past the name
    assert.strictEqual(byteToCharColumn("# @name 😀x", 13), 10); // an astral character is four bytes and two UTF-16 units
    assert.strictEqual(byteToCharColumn("ab", 10), 9); // past the end: keep going
    const p = toProblem({ path: "a.http", line: 2, column: 9, end_line: 2, end_column: 14, severity: "warning", message: "dup" }, ["### a", "# @name café"]);
    assert.deepStrictEqual([p.startColumn, p.endColumn], [8, 12]);
    const byPath = problemsByPath(
      { ok: false, files: 1, requests: 1, diagnostics: [{ path: "a.http", line: 2, column: 9, end_line: 2, end_column: 14, severity: "warning", message: "dup" }] },
      (path) => (path === "a.http" ? ["### a", "# @name café"] : []),
    );
    assert.deepStrictEqual([byPath.get("a.http")![0].startColumn, byPath.get("a.http")![0].endColumn], [8, 12]);
  });

  test("a project apic cannot load is one error on the file its message names", () => {
    assert.deepStrictEqual(parseUsageError("error: apic.yaml: yaml: line 1: did not find expected ',' or ']'\n"), {
      path: "apic.yaml",
      line: 0,
      message: "yaml: line 1: did not find expected ',' or ']'",
    });
    assert.deepStrictEqual(parseUsageError("error: users.http:12: @name needs a value (run `apic validate`)"), {
      path: "users.http",
      line: 11,
      message: "@name needs a value (run `apic validate`)",
    });
    assert.deepStrictEqual(parseUsageError("error: http-client.env.json: invalid character '}'"), { path: "http-client.env.json", line: 0, message: "invalid character '}'" });
    assert.deepStrictEqual(parseUsageError("error: environment \"prod\" not found"), { path: "apic.yaml", line: 0, message: 'environment "prod" not found' });
    assert.strictEqual(parseUsageError(""), undefined);
    assert.strictEqual(parseUsageError("some warning"), undefined);
  });

  test("reads the --json error object apic writes on stderr", () => {
    const obj = JSON.stringify({ error: { code: "E205", title: "project configuration problem", message: "apic.yaml: yaml: line 1: bad", hint: "Fix apic.yaml", exit: 2, url: "https://datagriff.github.io/api-caller/errors/#e205" } });
    assert.deepStrictEqual(parseUsageError(obj + "\n"), { path: "apic.yaml", line: 0, message: "yaml: line 1: bad" });
    assert.deepStrictEqual(parseStderrError("apic mcp listening\n" + obj), {
      message: "apic.yaml: yaml: line 1: bad",
      code: "E205",
      title: "project configuration problem",
      hint: "Fix apic.yaml",
      url: "https://datagriff.github.io/api-caller/errors/#e205",
    });
    assert.deepStrictEqual(parseStderrError("error: a.http:1: missing variable\n  {{token}}: pass --var token=...\n"), {
      message: "a.http:1: missing variable\n{{token}}: pass --var token=...",
    });
    assert.strictEqual(parseStderrError('{"not": "an error"}'), undefined);
  });

  test("rejects output that is not the validate shape", () => {
    assert.strictEqual(parseValidateOutput(""), undefined);
    assert.strictEqual(parseValidateOutput("not json"), undefined);
    assert.strictEqual(parseValidateOutput('{"ok": true}'), undefined);
    assert.strictEqual(parseValidateOutput('{"ok": true, "diagnostics": []}')?.diagnostics.length, 0);
  });
});

suite("did you mean", () => {
  test("levenshtein", () => {
    assert.strictEqual(levenshtein("", "abc"), 3);
    assert.strictEqual(levenshtein("kitten", "sitting"), 3);
    assert.strictEqual(levenshtein("name", "name"), 0);
  });

  test("suggests the nearest known directive for a typo, nothing for a stranger", () => {
    const known = ["name", "description", "capture", "assert", "auth", "step", "ref", "forceRef", "no-redirect", "no-session", "timeout", "retry", "note", "prompt"];
    assert.strictEqual(nearestDirective("nmae", known), "name");
    assert.strictEqual(nearestDirective("asert", known), "assert");
    assert.strictEqual(nearestDirective("Timeout", known), "timeout");
    assert.strictEqual(nearestDirective("forceref", known), "forceRef");
    assert.strictEqual(nearestDirective("frobnicate", known), undefined);
  });

  test("reads the known directives from the shipped grammar", () => {
    const grammar = fs.readFileSync(path.join(__dirname, "..", "..", "..", "syntaxes", "apic-directives.injection.json"), "utf8");
    const known = directivesFromGrammar(grammar);
    for (const want of ["name", "assert", "capture", "ref", "retry"]) {
      assert.ok(known.includes(want), `${want} missing from ${known.join(",")}`);
    }
    assert.deepStrictEqual(directivesFromGrammar("{}"), []);
    assert.deepStrictEqual(directivesFromGrammar("nope"), []);
  });
});
