import * as assert from "node:assert";
import { authItems, bodyPathItems, contextAt, directiveItems, lastBodyFor, operatorItems, selectorItems, variableItems } from "../../complete";
import { hoverText, placeholderAt } from "../../hoverText";
import type { Description, RunResult } from "../../types";

suite("completion context", () => {
  test("a directive after # @ or // @", () => {
    assert.deepStrictEqual(contextAt("# @ass", 6), { kind: "directive", prefix: "ass", start: 2 });
    assert.deepStrictEqual(contextAt("  // @", 6), { kind: "directive", prefix: "", start: 5 });
    assert.strictEqual(contextAt("# @assert status ", 17)?.kind, "operator");
    assert.strictEqual(contextAt("GET http://x @", 14), undefined);
  });

  test("a variable inside {{, closed or not", () => {
    assert.deepStrictEqual(contextAt("GET {{ba", 8, ""), { kind: "variable", prefix: "ba", start: 6, closed: false });
    assert.deepStrictEqual(contextAt("GET {{}}/x", 6, "}}/x"), { kind: "variable", prefix: "", start: 6, closed: true });
    assert.strictEqual(contextAt("GET {{baseUrl}}/x", 17), undefined, "after the closing braces nothing is offered");
    // Inside a directive value too.
    assert.strictEqual(contextAt("# @assert body.$.id == {{", 25)?.kind, "variable");
  });

  test("a selector after @assert and @capture x =", () => {
    assert.deepStrictEqual(contextAt("# @assert ", 10), { kind: "selector", prefix: "", start: 10, directive: "assert" });
    assert.deepStrictEqual(contextAt("# @assert body.$.it", 19), { kind: "selector", prefix: "body.$.it", start: 10, directive: "assert" });
    assert.deepStrictEqual(contextAt("# @capture token = bo", 21), { kind: "selector", prefix: "bo", start: 19, directive: "capture" });
    // Right after the `=`, before the space: `=` is a trigger character.
    assert.deepStrictEqual(contextAt("# @capture token =", 18), { kind: "selector", prefix: "", start: 18, directive: "capture" });
    assert.strictEqual(contextAt("# @capture token", 16), undefined, "the name of a capture is free text, nothing to offer yet");
  });

  test("operators, auth types and ref targets", () => {
    assert.deepStrictEqual(contextAt("# @assert status =", 18), { kind: "operator", prefix: "=", start: 17 });
    assert.deepStrictEqual(contextAt("# @auth be", 10), { kind: "auth", prefix: "be", start: 8 });
    assert.deepStrictEqual(contextAt("# @forceRef lo", 14), { kind: "ref", prefix: "lo", start: 12 });
    assert.strictEqual(contextAt("# @assert status == 200", 23), undefined, "the value is free text");
  });
});

suite("completion items", () => {
  test("every known directive gets an item, with a snippet body when one is known", () => {
    const items = directiveItems(["name", "assert", "brand-new"]);
    assert.deepStrictEqual(
      items.map((i) => i.label),
      ["@name", "@assert", "@brand-new"],
    );
    assert.strictEqual(items[1].insert, "@assert ${1:status} ${2|==,!=,<,<=,>,>=,contains,startsWith,endsWith,matches,exists,not exists|} ${3:200}");
    assert.strictEqual(items[2].insert, "@brand-new ");
    assert.ok(items[0].documentation?.includes("command line"));
    // File order is kept, so the common ones stay at the top.
    assert.ok(items[0].sortText! < items[2].sortText!);
  });

  test("variables from env, session, built-ins and the file's requests", () => {
    const items = variableItems(
      {
        env: [
          { name: "baseUrl", value: "http://x", source: "http-client.env.json [dev]" },
          { name: "password", value: "***", source: "http-client.private.env.json [dev]", secret: true },
          { name: "token", source: "session", missing: true },
        ],
        session: { token: "t-1", other: "1", "$oauth2:abc": "cached" },
        requests: ["login"],
      },
      false,
    );
    const byLabel = new Map(items.map((i) => [i.label, i]));
    assert.strictEqual(byLabel.get("baseUrl")?.insert, "baseUrl}}");
    assert.strictEqual(byLabel.get("baseUrl")?.detail, "http-client.env.json [dev]");
    assert.strictEqual(byLabel.get("password")?.documentation, "= ***");
    assert.strictEqual(byLabel.get("token")?.documentation, "not set", "env wins over the session for the same name");
    assert.strictEqual(byLabel.get("other")?.detail, "session");
    assert.ok(!byLabel.has("$oauth2:abc"), "cached tokens are not placeholders");
    assert.strictEqual(byLabel.get("$uuid")?.detail, "built-in");
    assert.strictEqual(byLabel.get("$randomInt")?.insert, "$randomInt ${1:1} ${2:100}}}");
    assert.strictEqual(byLabel.get("login.response.body.$")?.insert, "login.response.body.$.${1:path}}}");
    // Env first, then built-ins, then references.
    assert.ok(byLabel.get("baseUrl")!.sortText! < byLabel.get("$uuid")!.sortText!);
    assert.ok(byLabel.get("$uuid")!.sortText! < byLabel.get("login.response.headers")!.sortText!);
    // With `}}` already there nothing is appended.
    assert.strictEqual(variableItems({ env: [{ name: "a", value: "1", source: "x" }] }, true)[0].insert, "a");
  });

  test("selectors, and the keys of the last body after body.$.", () => {
    assert.deepStrictEqual(
      selectorItems("").map((i) => i.label),
      ["status", "statusText", "header.", "cookie.", "body", "body.$", "body.$.", "duration"],
    );
    const body = { id: 7, items: [{ name: "a" }], "key with dots": true, nested: { deep: null } };
    assert.deepStrictEqual(
      bodyPathItems("body.$.", body).map((i) => [i.label, i.detail]),
      [
        ["body.$.id", "7"],
        ["body.$.items", "array of 1"],
        ['body.$["key with dots"]', "true"],
        ["body.$.nested", "object"],
      ],
    );
    assert.deepStrictEqual(
      bodyPathItems("body.$.items.", body).map((i) => i.label),
      ["body.$.items.#", "body.$.items[0]"],
    );
    assert.deepStrictEqual(
      bodyPathItems("body.$.items[0].", body).map((i) => i.label),
      ["body.$.items[0].name"],
    );
    // An index being typed: the array's own items, replacing the `[`.
    assert.deepStrictEqual(
      bodyPathItems("body.$.items[", body).map((i) => i.label),
      ["body.$.items.#", "body.$.items[0]"],
    );
    assert.deepStrictEqual(
      bodyPathItems("body.$.items[1", body).map((i) => i.label),
      ["body.$.items.#", "body.$.items[0]"],
    );
    assert.deepStrictEqual(bodyPathItems("body.$.nope.", body), []);
    assert.deepStrictEqual(bodyPathItems("body.$.", undefined), []);
    // Typed halfway through a key: the same level is offered, the editor filters.
    assert.deepStrictEqual(
      bodyPathItems("body.$.it", body).map((i) => i.label),
      ["body.$.id", "body.$.items", 'body.$["key with dots"]', "body.$.nested"],
    );
    // selectorItems falls back to the plain list when the body has nothing at the path.
    assert.strictEqual(selectorItems("body.$.nope.", body)[0].label, "status");
    assert.strictEqual(selectorItems("body.$.", body)[0].label, "body.$.id");
  });

  test("the last body for a request, JSON only", () => {
    const results: RunResult[] = [
      { ok: true, request: { name: "login", file: "a.http", line: 1, method: "POST", url: "u" }, response: { status: 200, status_text: "OK", headers: {}, body: { token: "t" }, duration_ms: 1, size: 1 } },
      { ok: true, request: { name: "text", file: "a.http", line: 1, method: "GET", url: "u" }, response: { status: 200, status_text: "OK", headers: {}, body: "plain", duration_ms: 1, size: 1 } },
    ];
    assert.deepStrictEqual(lastBodyFor(results, "login"), { token: "t" });
    // Ran twice in the shown run (once as a # @ref dependency): the latest body wins.
    const again: RunResult = { ...results[0], response: { ...results[0].response!, body: { token: "t-2" } } };
    assert.deepStrictEqual(lastBodyFor([...results, again], "login"), { token: "t-2" });
    assert.strictEqual(lastBodyFor(results, "text"), undefined);
    assert.strictEqual(lastBodyFor(results, undefined), undefined);
  });

  test("operators and auth types", () => {
    assert.strictEqual(operatorItems().length, 23);
    assert.ok(operatorItems().some((i) => i.label === "matchesSchema"));
    assert.strictEqual(authItems().find((i) => i.label === "bearer")?.insert, "bearer {{${1:token}}}");
    // The key is positional, the header an option: `apikey <key> header=…`.
    assert.strictEqual(authItems().find((i) => i.label === "apikey")?.insert, "apikey {{${1:apiKey}}} header=${2:X-Api-Key}");
  });
});

const description: Description = {
  id: "whoami",
  file: "auth.http",
  line: 9,
  method: "GET",
  url_template: "{{baseUrl}}/me",
  url: "http://x/me",
  headers: {},
  variables: [
    { name: "baseUrl", value: "http://x", source: "http-client.env.json [dev]" },
    { name: "password", value: "***", source: "http-client.private.env.json [dev]", secret: true },
    { name: "token", source: "", missing: true, captured_by: "login" },
    { name: "other", source: "", missing: true, captured_by: "login", ref_runs: true },
  ],
  ready: false,
};

suite("hover", () => {
  test("finds the placeholder under the column", () => {
    const line = "GET {{baseUrl}}/users/{{ id }}";
    assert.deepStrictEqual(placeholderAt(line, 6), { name: "baseUrl", start: 4, end: 15 });
    assert.deepStrictEqual(placeholderAt(line, 15), { name: "baseUrl", start: 4, end: 15 }, "the closing brace counts");
    assert.deepStrictEqual(placeholderAt(line, 26), { name: "id", start: 22, end: 30 });
    assert.strictEqual(placeholderAt(line, 18), undefined);
  });

  test("says the value and source, masks secrets, names the capturing request", () => {
    assert.strictEqual(hoverText("baseUrl", { description }), "**baseUrl** = `http://x`\n\nfrom http-client.env.json [dev]");
    assert.strictEqual(hoverText("password", { description }), "**password** = `***`\n\nfrom http-client.private.env.json [dev] (secret, masked)");
    assert.strictEqual(hoverText("token", { description }), "**token** · not set\n\nCaptured by `login`: run it first, or add `# @ref login`.");
    assert.strictEqual(hoverText("other", { description }), "**other** · not set\n\nCaptured by `login`, which `# @ref` runs first.");
    assert.ok(hoverText("nothing", { description })?.includes("No source defines it"));
    assert.strictEqual(hoverText("nothing", {}), undefined, "outside a request with no env, nothing to say");
    assert.strictEqual(hoverText("baseUrl", { env: [{ name: "baseUrl", value: "http://y", source: ".env" }] }), "**baseUrl** = `http://y`\n\nfrom .env");
  });

  test("built-ins and response references", () => {
    assert.strictEqual(hoverText("$uuid", {}), "**$uuid** · built-in\n\nrandom UUID v4");
    assert.ok(hoverText("$datetime rfc1123", {})?.startsWith("**$datetime**"));
    assert.ok(hoverText("$env.HOME", {})?.includes("shell environment"));
    assert.ok(hoverText("$nope", {})?.includes("unknown"));
    assert.ok(hoverText('$auth.token("api")', {})?.includes("Security.Auth"));
    assert.strictEqual(hoverText("login.response.body.$.token", {}), "**login.response.body.$.token** · response reference\n\nThe body of `login` from earlier in the same run.");
  });
});
