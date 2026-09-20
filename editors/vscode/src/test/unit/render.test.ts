import * as assert from "node:assert";
import { bodyText, escapeHtml, firstProblem, highlightJson, rawBody, renderDescription, renderRun, size, BODY_LIMIT } from "../../render";
import type { Description, RunResult } from "../../types";

const login: RunResult = {
  ok: true,
  request: { name: "login", file: "auth.http", line: 9, method: "POST", url: "http://localhost:8089/auth/login", headers: { "Content-Type": "application/json" }, body: '{"user": "alice", "password": "s3cret"}' },
  response: { status: 200, status_text: "OK", headers: { "content-type": "application/json", "content-length": "30" }, body: { access_token: "mock-token" }, duration_ms: 1, size: 30 },
  captures: { token: "mock-token" },
  asserts: [{ expr: "status == 200", pass: true, actual: "200", expected: "200" }],
};

const notFound: RunResult = {
  ok: false,
  request: { name: "not-found", file: "todos.http", line: 91, method: "GET", url: "http://localhost:8089/status/404", headers: {} },
  response: { status: 404, status_text: "Not Found", headers: { "content-type": "application/json" }, body: { status: 404 }, duration_ms: 1, size: 15 },
  asserts: [{ expr: "status == 200", pass: false, actual: "404", expected: "200" }],
};

const unsent: RunResult = {
  ok: false,
  request: { name: "get-job", file: "jobs.http", line: 16, method: "GET", url: "{{baseUrl}}/jobs/{{jobId}}" },
  errors: ["jobs.http:16: missing variable\n  {{jobId}}: it is captured by request \"create-job\""],
};

suite("rendering a run", () => {
  test("one result: status line, headers, highlighted body, checks and captures", () => {
    const html = renderRun([login]);
    assert.ok(html.startsWith('<div class="run"><section class="result"><h2>'), html.slice(0, 80));
    for (const want of [
      '<span class="method POST">POST</span>',
      '<span class="status s2">200 OK</span>',
      "· 1 ms · 30 B",
      "<summary>Request</summary>",
      "Response headers (2)",
      '<span class="key">&quot;access_token&quot;</span>: <span class="str">&quot;mock-token&quot;</span>',
      '<button data-action="toggle-raw" data-index="0">Raw</button>',
      '<button data-action="save" data-index="0">Save body…</button>',
      '<pre class="body raw hidden" data-index="0">{&quot;access_token&quot;:&quot;mock-token&quot;}</pre>',
      '<li class="pass"><span class="mark pass">✓</span> <code>status == 200</code></li>',
      '<span class="capture">↳ token</span> = <code>mock-token</code>',
    ]) {
      assert.ok(html.includes(want), `missing ${want} in\n${html}`);
    }
    assert.ok(!html.includes("<table>"), "a single result has no flow summary");
    // Deterministic: the same input renders the same HTML.
    assert.strictEqual(renderRun([login]), html);
  });

  test("a failure shows actual against expected; an unsent request shows its errors", () => {
    const html = renderRun([notFound]);
    assert.ok(html.includes('<span class="status s4">404 Not Found</span>'));
    assert.ok(html.includes('<span class="dim">actual</span> <code>404</code> <span class="dim">expected</span> <code>200</code>'));
    const html2 = renderRun([unsent]);
    assert.ok(html2.includes('<span class="status s4">not sent</span>'));
    assert.ok(html2.includes("missing variable"));
    assert.ok(!html2.includes("Save body"), "no body, no save button");
  });

  test("a flow gets a summary table and one collapsible block per request", () => {
    const html = renderRun([login, notFound], { title: "todos.http", redact: true });
    assert.ok(html.includes('<p class="badge">redacted</p>'));
    assert.ok(html.includes("<h1>todos.http</h1>"));
    assert.ok(html.includes('<span class="fail">1 failed</span>, 1 passed'));
    assert.ok(html.includes("· 2 requests · 2 ms"));
    assert.ok(html.includes("status == 200 (actual: 404)"));
    assert.strictEqual((html.match(/<details class="result" open>/g) ?? []).length, 2);
  });

  test("a dependency a # @ref ran first is rendered before the request that needed it", () => {
    const html = renderRun([{ ...notFound, ran_first: [login] }]);
    assert.ok(html.indexOf("auth/login") < html.indexOf("status/404"));
    assert.ok(html.includes("↳ ran login first (# @ref)"));
  });

  test("oversized bodies are truncated with a way to open them", () => {
    const big = { ...login, response: { ...login.response!, body: "x".repeat(BODY_LIMIT + 1) } };
    const html = renderRun([big]);
    assert.ok(html.includes('data-action="open-raw"'));
    assert.ok(html.length < BODY_LIMIT, "the preview is bounded");
  });

  test("helpers", () => {
    assert.strictEqual(escapeHtml('<a href="x">&</a>'), "&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;");
    assert.deepStrictEqual(bodyText('{"a":1}'), { text: '{\n  "a": 1\n}', json: true });
    assert.deepStrictEqual(bodyText("plain"), { text: "plain", json: false });
    assert.deepStrictEqual(bodyText(null), { text: "", json: false });
    assert.strictEqual(rawBody({ a: 1 }), '{"a":1}');
    assert.strictEqual(rawBody("csv,rows"), "csv,rows");
    assert.strictEqual(highlightJson('{"n": -1.5e3, "t": true}'), '{<span class="key">&quot;n&quot;</span>: <span class="num">-1.5e3</span>, <span class="key">&quot;t&quot;</span>: <span class="lit">true</span>}');
    assert.strictEqual(size(999), "999 B");
    assert.strictEqual(size(2048), "2.0 kB");
    assert.strictEqual(size(3 * 1024 * 1024), "3.0 MB");
    assert.strictEqual(firstProblem(notFound), "status == 200 (actual: 404)");
    assert.strictEqual(firstProblem(unsent), unsent.errors![0]);
    assert.strictEqual(firstProblem(login), "");
  });
});

suite("rendering a description", () => {
  const d: Description = {
    name: "whoami",
    id: "whoami",
    file: "auth.http",
    line: 20,
    description: "Who am I",
    method: "GET",
    url_template: "{{baseUrl}}/me",
    url: "{{baseUrl}}/me",
    headers: { Authorization: "Bearer {{token}}" },
    variables: [
      { name: "token", source: "missing", missing: true, captured_by: "login", ref_runs: true },
      { name: "baseUrl", value: "http://localhost:8089", source: "http-client.env.json [local]" },
      { name: "password", value: "s3cret", source: "http-client.private.env.json [local]", secret: true },
    ],
    asserts: ["status == 200"],
    refs: ["login"],
    auth: "bearer {{token}}",
    auth_source: "apic.yaml",
    ready: true,
  };

  test("shows readiness, every variable with its source, secrets masked, and the sections", () => {
    const html = renderDescription(d);
    for (const want of [
      '<p class="badge pass">ready</p>',
      "captured by login, which # @ref runs first",
      "<code>http://localhost:8089</code>",
      "<code>***</code>",
      "http-client.private.env.json [local]",
      "<h3>auth</h3><pre>bearer {{token}} <span class=\"dim\">(apic.yaml)</span></pre>",
      "<li># @ref login</li>",
      "<li>status == 200</li>",
      "Authorization: Bearer {{token}}",
    ]) {
      assert.ok(html.includes(want), `missing ${want} in\n${html}`);
    }
    assert.ok(!html.includes("s3cret"), "a secret value never reaches the panel");
  });

  test("a request that is not ready names what is missing", () => {
    const html = renderDescription({ ...d, ready: false, refs: [], variables: [{ name: "jobId", source: "missing", missing: true, captured_by: "create-job" }] });
    assert.ok(html.includes('<p class="badge fail">not ready · missing {{jobId}}</p>'));
    assert.ok(html.includes("captured by create-job — run it first"));
  });
});
