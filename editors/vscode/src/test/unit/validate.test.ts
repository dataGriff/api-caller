// Runs under plain node (npm run test:unit): nothing here imports vscode.
import * as assert from "node:assert";
import * as fs from "node:fs";
import * as path from "node:path";
import { directivesFromGrammar, levenshtein, nearestDirective, parseValidateOutput, problemsByPath, LINE_END } from "../../validate";

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
