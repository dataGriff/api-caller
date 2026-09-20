import * as assert from "node:assert";
import { normalizeRelative, positionsFromList, requestAt } from "../../lens";
import type { ListOutput } from "../../types";

const list: ListOutput = {
  root: "/p",
  requests: [
    { id: "login", name: "login", method: "POST", url: "{{baseUrl}}/auth/login", file: "auth.http", line: 9 },
    { id: "whoami", name: "whoami", method: "GET", url: "{{baseUrl}}/me", file: "auth.http", line: 20 },
    { id: "explore.http#1", method: "GET", url: "{{baseUrl}}/health", file: "explore.http", line: 3 },
    { id: "explore.http#2", method: "GET", url: "{{baseUrl}}/slow", file: "explore.http", line: 8 },
    { id: "deep", name: "deep", method: "GET", url: "{{baseUrl}}/todos", file: "nested/deep.http", line: 3 },
  ],
};

suite("positions from apic list", () => {
  test("keeps the requests of one file, named as file#name and unnamed as file#N", () => {
    assert.deepStrictEqual(positionsFromList(list, "auth.http"), [
      { target: "auth.http#login", name: "login", line: 9, method: "POST" },
      { target: "auth.http#whoami", name: "whoami", line: 20, method: "GET" },
    ]);
    assert.deepStrictEqual(
      positionsFromList(list, "explore.http").map((p) => p.target),
      ["explore.http#1", "explore.http#2"],
    );
  });

  test("matches paths the way apic prints them whatever the OS wrote", () => {
    assert.strictEqual(normalizeRelative("nested\\deep.http"), "nested/deep.http");
    assert.strictEqual(normalizeRelative("./auth.http"), "auth.http");
    assert.strictEqual(positionsFromList(list, "nested\\deep.http")[0]?.target, "nested/deep.http#deep");
    assert.deepStrictEqual(positionsFromList(list, "missing.http"), []);
  });
});

// The file the positions below describe; apic reported request lines 6, 13
// and 18. Line 15's block has a request line apic did not report (say it
// failed to parse), so the cursor there has nothing to run.
const text = [
  "@baseUrl = http://x", // 1
  "", // 2
  "### Log in", // 3
  "# @name login", // 4
  "# @assert status == 200", // 5
  "POST {{baseUrl}}/auth/login", // 6
  "Content-Type: application/json", // 7
  "", // 8
  '{"user": "a"}', // 9
  "", // 10
  "### unnamed", // 11
  "// a comment", // 12
  "{{baseUrl}}/health HTTP/1.1", // 13
  "", // 14
  "###", // 15
  "# @name broken", // 16
  "# @capture nope", // 17
  "DELETE {{baseUrl}}/x", // 18
].join("\n");

const positions = positionsFromList(
  {
    root: "/p",
    requests: [
      { id: "login", name: "login", method: "POST", url: "{{baseUrl}}/auth/login", file: "api.http", line: 6 },
      { id: "api.http#2", method: "GET", url: "{{baseUrl}}/health", file: "api.http", line: 13 },
      { id: "broken", name: "broken", method: "DELETE", url: "{{baseUrl}}/x", file: "api.http", line: 18 },
    ],
  },
  "api.http",
);

suite("the request under the cursor", () => {
  test("a directive above a request line belongs to that request", () => {
    assert.strictEqual(requestAt(positions, 4, text)?.name, "login");
    assert.strictEqual(requestAt(positions, 3, text)?.name, "login"); // the separator itself
    assert.strictEqual(requestAt(positions, 12, text)?.target, "api.http#2");
    assert.strictEqual(requestAt(positions, 17, text)?.name, "broken");
  });

  test("the request line and its headers and body belong to it", () => {
    assert.strictEqual(requestAt(positions, 6, text)?.name, "login");
    assert.strictEqual(requestAt(positions, 7, text)?.name, "login");
    assert.strictEqual(requestAt(positions, 9, text)?.name, "login");
    assert.strictEqual(requestAt(positions, 10, text)?.name, "login");
  });

  test("nothing above the first request, and nothing in a block apic did not report", () => {
    assert.strictEqual(requestAt(positions, 1, text), undefined);
    assert.strictEqual(requestAt([], 6, text), undefined);
    const withoutBroken = positions.filter((p) => p.name !== "broken");
    assert.strictEqual(requestAt(withoutBroken, 16, text), undefined);
    assert.strictEqual(requestAt(withoutBroken, 18, text), undefined);
  });
});
