import * as assert from "node:assert";
import { normalizeRelative, positionsFromList, requestAt, scanRequests } from "../../lens";
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
  "# @assert", // 17
  "DELETE {{baseUrl}}/x", // 18
].join("\n");

suite("scanning a file without the parser", () => {
  test("finds each block's request line, its name and its method", () => {
    assert.deepStrictEqual(scanRequests(text, "api.http"), [
      { target: "api.http#login", name: "login", line: 6, method: "POST" },
      { target: "api.http#2", name: undefined, line: 13, method: "GET" },
      { target: "api.http#broken", name: "broken", line: 18, method: "DELETE" },
    ]);
  });

  test("a file with no separator is one request", () => {
    assert.deepStrictEqual(scanRequests("# @name only\nGET http://x\n", "one.http"), [{ target: "one.http#only", name: "only", line: 2, method: "GET" }]);
    assert.deepStrictEqual(scanRequests("", "empty.http"), []);
  });

  test("the request under a line is the one whose block holds it", () => {
    const positions = scanRequests(text, "api.http");
    assert.strictEqual(requestAt(positions, 4, text)?.name, "login"); // a directive above the request line
    assert.strictEqual(requestAt(positions, 9, text)?.name, "login"); // the body
    assert.strictEqual(requestAt(positions, 12, text)?.target, "api.http#2");
    assert.strictEqual(requestAt(positions, 17, text)?.name, "broken");
    assert.strictEqual(requestAt(positions, 1, text), undefined); // the file variable above the first block
  });
});
