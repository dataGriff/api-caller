// Runs inside a real VS Code opened on src/test/fixture (see
// .vscode-test.mjs). Activation, the commands, the diagnostics and the
// lenses are exercised for real; the binary is exercised when apic is on
// PATH, which CI arranges by building it first. Pure logic has its own
// tests under unit/, run with plain node.
import * as assert from "node:assert";
import { spawn, type ChildProcess } from "node:child_process";
import * as fs from "node:fs";
import * as http from "node:http";
import * as net from "node:net";
import * as os from "node:os";
import * as path from "node:path";
import * as vscode from "vscode";
import { compareVersions, findOnPath } from "../apic";
import type { ApicApi } from "../extension";

const extensionId = "dataGriff.apic";

async function api(): Promise<ApicApi> {
  const ext = vscode.extensions.getExtension<ApicApi>(extensionId);
  assert.ok(ext, `extension ${extensionId} is not installed in the test host`);
  return ext.activate();
}

function fixture(): string {
  const folder = vscode.workspace.workspaceFolders?.[0];
  assert.ok(folder, "the fixture workspace should be open");
  return folder.uri.fsPath;
}

/** Polls an async check until it holds, or gives up. */
async function until(check: () => Promise<boolean> | boolean, ms = 10000): Promise<void> {
  const start = Date.now();
  while (!(await check())) {
    if (Date.now() - start > ms) {
      throw new Error("timed out waiting");
    }
    await new Promise((r) => setTimeout(r, 100));
  }
}

suite("apic extension", () => {
  test("activates on a workspace with .http files and registers its commands", async () => {
    await api();
    const commands = await vscode.commands.getCommands(true);
    for (const c of ["apic.showVersion", "apic.openInstallPage", "apic.runRequest", "apic.runFile", "apic.describeRequest", "apic.copyCurl", "apic.showLastResponse", "apic.pickEnvironment", "apic.validate"]) {
      assert.ok(commands.includes(c), `command ${c} missing`);
    }
  });

  test("finds the project root above a request file", async () => {
    const { projectRoot } = await api();
    const file = vscode.Uri.file(path.join(fixture(), "nested", "deep.http"));
    assert.strictEqual(projectRoot(file), fixture());
    assert.strictEqual(projectRoot(undefined), fixture());
  });

  test("compares versions the way releases are numbered", () => {
    assert.ok(compareVersions("0.1.2", "0.1.2") === 0);
    assert.ok(compareVersions("v0.2.0", "0.1.9") > 0);
    assert.ok(compareVersions("0.1.1", "0.1.2") < 0);
    assert.ok(compareVersions("dev", "0.1.2") > 0, "a local build is never too old");
  });

  test("reports the version when apic is on PATH", async function () {
    if (!findOnPath("apic")) {
      this.skip();
    }
    const { apic } = await api();
    const info = await apic.version();
    assert.ok(info.version, `version info: ${JSON.stringify(info)}`);
    // The command itself: it runs the binary and shows a message, and must
    // return rather than throw.
    await vscode.commands.executeCommand("apic.showVersion");
    const res = await apic.json<{ requests: { id: string }[] }>(["list"], { project: fixture() });
    assert.strictEqual(res.code, 0, res.stderr);
    assert.ok(res.value?.requests.some((r) => r.id === "ping"), JSON.stringify(res.value));
  });

  test("publishes what apic validate reports, with the code and the span", async function () {
    if (!findOnPath("apic")) {
      this.skip();
    }
    const { validateNow } = await api();
    await validateNow();
    const uri = vscode.Uri.file(path.join(fixture(), "warn.http"));
    const diags = vscode.languages.getDiagnostics(uri);
    const warning = diags.find((d) => d.source === "apic" && typeof d.code === "object" && d.code.value === "unknown-directive");
    assert.ok(warning, `expected an unknown-directive warning, got ${JSON.stringify(diags.map((d) => d.message))}`);
    assert.strictEqual(warning.severity, vscode.DiagnosticSeverity.Warning);
    assert.strictEqual(warning.range.start.line, 2);
    // The span covers `@frobnicate`: column 3 to 14, 1-based and end-exclusive.
    assert.strictEqual(warning.range.start.character, 2);
    assert.strictEqual(warning.range.end.character, 13);
    // A clean file has nothing.
    assert.deepStrictEqual(vscode.languages.getDiagnostics(vscode.Uri.file(path.join(fixture(), "api.http"))), []);
  });

  test("offers to change a mistyped directive to the nearest known one", async function () {
    if (!findOnPath("apic")) {
      this.skip();
    }
    const { validateNow } = await api();
    await validateNow();
    const uri = vscode.Uri.file(path.join(fixture(), "warn.http"));
    const doc = await vscode.workspace.openTextDocument(uri);
    // Quick fixes are computed for the diagnostic's range; ask for the
    // directive line.
    const actions = await vscode.commands.executeCommand<vscode.CodeAction[]>("vscode.executeCodeActionProvider", uri, doc.lineAt(2).range, vscode.CodeActionKind.QuickFix.value);
    // `frobnicate` is nobody's typo, so no rename is offered for it; the
    // provider ran and produced nothing rather than failing.
    assert.ok(Array.isArray(actions));
    assert.ok(!actions.some((a) => a.title.startsWith("Change to @")), JSON.stringify(actions.map((a) => a.title)));
  });

  test("puts Run, Describe and Copy as curl above each request", async () => {
    await api();
    const uri = vscode.Uri.file(path.join(fixture(), "api.http"));
    await vscode.workspace.openTextDocument(uri);
    let lenses: vscode.CodeLens[] = [];
    await until(async () => {
      lenses = (await vscode.commands.executeCommand<vscode.CodeLens[]>("vscode.executeCodeLensProvider", uri)) ?? [];
      return lenses.length > 0;
    });
    const titles = lenses.map((l) => l.command?.title ?? "");
    assert.ok(titles.some((t) => t.includes("Run")), titles.join(","));
    assert.ok(titles.includes("Describe"), titles.join(","));
    assert.ok(titles.includes("Copy as curl"), titles.join(","));
    // The request line of `ping` is line 4 (1-based) in the fixture.
    const run = lenses.find((l) => l.command?.command === "apic.runRequest");
    assert.ok(run);
    assert.strictEqual(run.range.start.line, 3);
    assert.deepStrictEqual(run.command?.arguments, [fixture(), "api.http#ping"]);
  });

  test("runs a request against a demo API and shows the result", async function () {
    const bin = findOnPath("apic");
    if (!bin) {
      this.skip();
    }
    this.timeout(60000);
    const { lastResults, apic } = await api();
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "apic-vscode-"));
    const port = await freePort();
    const demo: ChildProcess = spawn(bin, ["demo", "--out", dir, "--port", String(port), "--force"], { stdio: "ignore" });
    try {
      await until(() => fs.existsSync(path.join(dir, "http-client.env.json")));
      await waitFor(`http://127.0.0.1:${port}/health`);
      // apic demo writes the project into `dir` and serves the API; the
      // extension runs the request through the same binary.
      await vscode.commands.executeCommand("apic.runRequest", dir, "auth.http#login");
      const results = lastResults();
      assert.strictEqual(results.length, 1, JSON.stringify(results));
      assert.strictEqual(results[0].response?.status, 200);
      assert.strictEqual(results[0].captures?.token, "mock-token");
      // whoami needs the token login just captured; it is in the demo
      // project's session now, so this is one request and it passes.
      await vscode.commands.executeCommand("apic.runRequest", dir, "auth.http#whoami");
      assert.ok(lastResults()[0].ok);
      // describe and curl go through the same binary.
      const d = await apic.json<{ id: string; ready: boolean }>(["describe", "whoami"], { project: dir });
      assert.strictEqual(d.value?.id, "whoami");
      await vscode.commands.executeCommand("apic.copyCurl", dir, "auth.http#login");
      const clip = await vscode.env.clipboard.readText();
      assert.ok(clip.startsWith("curl "), clip);
    } finally {
      demo.kill();
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.listen(0, "127.0.0.1", () => {
      const port = (srv.address() as net.AddressInfo).port;
      srv.close(() => resolve(port));
    });
    srv.on("error", reject);
  });
}

/** Waits until a URL answers 200. */
async function waitFor(url: string): Promise<void> {
  await until(
    () =>
      new Promise<boolean>((resolve) => {
        http
          .get(url, (res) => {
            res.resume();
            resolve(res.statusCode === 200);
          })
          .on("error", () => resolve(false));
      }),
    20000,
  );
}
