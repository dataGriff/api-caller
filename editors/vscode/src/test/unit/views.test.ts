import * as assert from "node:assert";
import type { ListOutput } from "../../types";
import { groupByFile, isTokenKey, requestDetail, requestLabel, sessionItems } from "../../views";

const list: ListOutput = {
  root: "/p",
  requests: [
    { id: "whoami", name: "whoami", method: "GET", url: "{{baseUrl}}/me", file: "auth.http", line: 20, asserts: 1 },
    { id: "explore.http#1", method: "GET", url: "{{baseUrl}}/health", file: "./explore.http", line: 3, description: "Is it up?" },
    { id: "login", name: "login", method: "POST", url: "{{baseUrl}}/auth/login", file: "auth.http", line: 9, captures: ["token"], asserts: 2 },
  ],
};

suite("requests view model", () => {
  test("groups by file, files sorted, requests in line order", () => {
    const groups = groupByFile(list);
    assert.deepStrictEqual(
      groups.map((g) => [g.file, g.requests.map((r) => r.id)]),
      [
        ["auth.http", ["login", "whoami"]],
        ["explore.http", ["explore.http#1"]],
      ],
    );
  });

  test("labels and details", () => {
    assert.strictEqual(requestLabel(list.requests[2]), "POST login");
    assert.strictEqual(requestLabel(list.requests[1]), "GET explore.http#1");
    assert.strictEqual(requestDetail(list.requests[2]), "2 asserts · captures token");
    assert.strictEqual(requestDetail(list.requests[0]), "1 assert");
    assert.strictEqual(requestDetail(list.requests[1]), "Is it up?");
  });
});

suite("session view model", () => {
  test("values sorted, tokens as apic describes them, cookies without values", () => {
    const items = sessionItems(
      { token: "tok-1", "$oauth2:abc": "token, expires in 59m", a: "1" },
      [{ name: "sid", domain: "api.example.com", path: "/", http_only: true }, { name: "pref", domain: "example.com", path: "/", expires: "2026-09-21T10:00:00Z" }],
      false,
    );
    assert.deepStrictEqual(items, [
      { name: "$oauth2:abc", shown: "token, expires in 59m", kind: "token" },
      { name: "a", shown: "1", kind: "value" },
      { name: "token", shown: "tok-1", kind: "value" },
      { name: "sid", shown: "*** (api.example.com/ · until cleared)", kind: "cookie" },
      { name: "pref", shown: "*** (example.com/ · expires 2026-09-21T10:00:00Z)", kind: "cookie" },
    ]);
    assert.ok(isTokenKey("$exec:1") && !isTokenKey("$meta"));
  });

  test("redacted runs mask captured values but keep the token descriptions", () => {
    const items = sessionItems({ token: "tok-1", "$exec:x": "token, expires in 5m" }, [], true);
    assert.deepStrictEqual(
      items.map((i) => i.shown),
      ["token, expires in 5m", "***"],
    );
  });
});
