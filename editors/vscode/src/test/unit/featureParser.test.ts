import * as assert from "node:assert";
import { parseFeature, rowLabel, stepRegex, stepUsages, testPathsFromYaml } from "../../featureParser";

const text = [
  "# a comment", // 1
  "@smoke @api", // 2
  "Feature: Todos", // 3
  "  Some description lines", // 4
  "  that are not steps.", // 5
  "", // 6
  "  Background:", // 7
  "    Given I am logged in", // 8
  "", // 9
  "  @slow", // 10
  "  Scenario: Create a todo", // 11
  '    When a todo titled "Ship apic" is created', // 12
  "    Then the response status is 201", // 13
  "    And the response body is:", // 14
  '      """', // 15
  '      Given this is not a step', // 16
  '      """', // 17
  "    And the variables:", // 18
  "      | name | value |", // 19
  "      | a    | 1     |", // 20
  "", // 21
  "  Scenario Outline: Status for <req>", // 22
  '    When I run "<req>"', // 23
  "    Then the response status is <status>", // 24
  "", // 25
  "    @first", // 26
  "    Examples: Good ones", // 27
  "      | req    | status |", // 28
  "      | health | 200    |", // 29
  "      | login  | 200    |", // 30
  "", // 31
  "  Rule: Later", // 32
  "  Example: A rule's example", // 33
  "    * something", // 34
].join("\n");

suite("feature parser", () => {
  test("reads feature, background, scenarios, outlines and tags", () => {
    const f = parseFeature(text)!;
    assert.strictEqual(f.name, "Todos");
    assert.strictEqual(f.line, 3);
    assert.deepStrictEqual(f.tags, ["@smoke", "@api"]);
    assert.deepStrictEqual(f.background?.steps.map((s) => [s.keyword, s.text, s.line]), [["Given", "I am logged in", 8]]);
    assert.deepStrictEqual(
      f.scenarios.map((s) => [s.keyword, s.name, s.line, s.tags]),
      [
        ["Scenario", "Create a todo", 11, ["@smoke", "@api", "@slow"]],
        ["Scenario Outline", "Status for <req>", 22, ["@smoke", "@api"]],
        ["Scenario", "A rule's example", 33, ["@smoke", "@api"]],
      ],
    );
    // Doc strings and data tables are not steps.
    assert.deepStrictEqual(
      f.scenarios[0].steps.map((s) => s.line),
      [12, 13, 14, 18],
    );
    const ex = f.scenarios[1].examples[0];
    assert.strictEqual(ex.name, "Good ones");
    assert.deepStrictEqual(ex.tags, ["@first"]);
    assert.deepStrictEqual(ex.header, ["req", "status"]);
    assert.deepStrictEqual(
      ex.rows.map((r) => [r.line, r.cells]),
      [
        [29, ["health", "200"]],
        [30, ["login", "200"]],
      ],
    );
    assert.strictEqual(rowLabel(ex.rows[0], ex.header), "req = health, status = 200");
    assert.deepStrictEqual(f.scenarios[2].steps.map((s) => s.text), ["something"]);
    assert.strictEqual(parseFeature("just text\n"), undefined);
    assert.strictEqual(parseFeature("Feature:\r\n  Scenario: crlf\r\n    When x\r\n")?.scenarios[0].steps[0].line, 3);
  });

  test("reads test.paths from apic.yaml, in both list shapes", () => {
    assert.deepStrictEqual(testPathsFromYaml("env: dev\ntest:\n  paths:\n    - features\n    - 'more/x.feature'\nauth:\n  default: none\n"), ["features", "more/x.feature"]);
    assert.deepStrictEqual(testPathsFromYaml("test:\n  paths: [a, \"b/c\"]\n"), ["a", "b/c"]);
    assert.deepStrictEqual(testPathsFromYaml("test:\n  paths: []\n"), []);
    assert.strictEqual(testPathsFromYaml("env: dev\n"), undefined);
    assert.strictEqual(testPathsFromYaml("test:\n  other: 1\nenv: dev\n"), undefined);
    assert.deepStrictEqual(testPathsFromYaml("test:\n  # the features\n  paths:\n    - features # default\n"), ["features"]);
  });

  test("turns a phrase into the regex apic uses", () => {
    assert.ok(stepRegex("a todo titled {title} is created").test('a todo titled "Ship apic" is created'));
    assert.ok(stepRegex("a todo titled {title} is created").test("a todo titled ship is created"));
    assert.ok(!stepRegex("a todo titled {title} is created").test("a todo titled ship it is created"), "an unquoted value is one word");
    assert.ok(stepRegex('user "{name}" exists').test('user "alice smith" exists'));
    assert.ok(!stepRegex('user "{name}" exists').test("user alice exists"));
    assert.ok(stepRegex("I am logged in").test("I am logged in"));
    assert.ok(!stepRegex("I am logged in").test("I am logged in again"));
    assert.ok(stepRegex("price is $5 (approx.)").test("price is $5 (approx.)"), "punctuation is literal");
  });

  test("counts the scenarios that use a phrase, background and outlines included", () => {
    const features = [{ uri: "/p/features/todos.feature", feature: parseFeature(text)! }];
    const logged = stepUsages(features, "I am logged in");
    assert.deepStrictEqual(logged, [{ uri: "/p/features/todos.feature", line: 8, label: "Todos › Background", scenarios: 3 }]);
    const created = stepUsages(features, "a todo titled {title} is created");
    assert.deepStrictEqual(created, [{ uri: "/p/features/todos.feature", line: 12, label: "Todos › Create a todo", scenarios: 1 }]);
    // An outline step matches through its example rows: `I run "<req>"` with req = health.
    const health = stepUsages(features, 'I run "{request}"');
    assert.deepStrictEqual(health.map((u) => [u.line, u.scenarios]), [[23, 2]]);
    assert.deepStrictEqual(stepUsages(features, "nobody says this"), []);
  });
});
