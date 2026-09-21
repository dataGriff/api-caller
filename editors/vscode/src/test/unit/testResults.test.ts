import * as assert from "node:assert";
import { outcomeLines, parseCucumber, sameFile, scenarioOutcomes, type CukeFeature } from "../../testResults";

// What apic test --json printed for a feature with a background, an
// outline (one element per example row, step lines of the outline) and
// an undefined step.
const cucumber: CukeFeature[] = [
  {
    uri: "/p/features/todos.feature",
    name: "Todos",
    elements: [
      { keyword: "Background", name: "", line: 5, type: "background", steps: [{ keyword: "Given ", name: "I am logged in", line: 6, result: { status: "passed", duration: 2_000_000 } }] },
      {
        keyword: "Scenario",
        name: "List",
        line: 8,
        type: "scenario",
        steps: [
          { keyword: "When ", name: "I list the todos", line: 9, result: { status: "passed", duration: 1_500_000 } },
          { keyword: "Then ", name: "the response status is 200", line: 10, result: { status: "passed", duration: 100_000 } },
        ],
      },
      {
        keyword: "Scenario Outline",
        name: "Status for <req>",
        line: 20,
        type: "scenario",
        steps: [
          { keyword: "When ", name: 'I run "health"', line: 14, result: { status: "passed", duration: 1_000_000 } },
          { keyword: "Then ", name: "the response status is 500", line: 15, result: { status: "failed", duration: 50_000, error_message: 'expected status == 500, got "200"\n  GET http://x/health' } },
        ],
      },
      {
        keyword: "Scenario",
        name: "Not written",
        line: 25,
        type: "scenario",
        steps: [
          { keyword: "When ", name: "I do something new", line: 26, result: { status: "undefined" } },
          { keyword: "Then ", name: "the response status is 200", line: 27, result: { status: "skipped" } },
        ],
      },
      {
        keyword: "Scenario",
        name: "All skipped",
        line: 30,
        type: "scenario",
        steps: [{ keyword: "When ", name: "x", line: 31, result: { status: "skipped" } }],
      },
    ],
  },
];

suite("cucumber results", () => {
  test("one outcome per scenario element, background folded in, failure at the step", () => {
    const out = scenarioOutcomes(cucumber);
    assert.deepStrictEqual(
      out.map((o) => [o.name, o.line, o.status, Math.round(o.durationMs * 100) / 100]),
      [
        ["List", 8, "passed", 3.6],
        ["Status for <req>", 20, "failed", 1.05],
        ["Not written", 25, "failed", 0],
        ["All skipped", 30, "skipped", 0],
      ],
    );
    assert.strictEqual(out[0].steps.length, 3, "the background step is part of the scenario");
    assert.deepStrictEqual(out[1].failure, { line: 15, message: 'expected status == 500, got "200"\n  GET http://x/health' });
    assert.strictEqual(out[2].failure?.line, 26);
    assert.ok(out[2].failure?.message.startsWith("Undefined step: When I do something new\n"), out[2].failure?.message);
    assert.ok(out[2].failure?.message.includes("apic test --steps"));
  });

  test("output lines mark every step", () => {
    const lines = outcomeLines(scenarioOutcomes(cucumber)[1]);
    assert.strictEqual(lines[0], "✗ Status for <req> (1 ms)");
    assert.strictEqual(lines[1], '    ✓ When I run "health"');
    assert.strictEqual(lines[2], "    ✗ Then the response status is 500");
    assert.strictEqual(lines[3], '        expected status == 500, got "200"');
  });

  test("parses only an array, and matches printed paths to files", () => {
    assert.strictEqual(parseCucumber(""), undefined);
    assert.strictEqual(parseCucumber("nope"), undefined);
    assert.strictEqual(parseCucumber("{}"), undefined);
    assert.strictEqual(parseCucumber("[]")?.length, 0);
    assert.ok(sameFile("/p/features/a.feature", "/p", "/p/features/a.feature"));
    assert.ok(sameFile("features/a.feature", "/p", "/p/features/a.feature"));
    assert.ok(sameFile("./features\\a.feature", "/p", "/p/features/a.feature"));
    assert.ok(!sameFile("features/b.feature", "/p", "/p/features/a.feature"));
  });
});
