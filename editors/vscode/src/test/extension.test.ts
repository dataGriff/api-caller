// Runs inside a real VS Code opened on src/test/fixture (see
// .vscode-test.mjs). Activation, the commands and the project discovery
// are exercised for real; the binary is exercised when apic is on PATH,
// which CI arranges by building it first.
import * as assert from "node:assert";
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

suite("apic extension", () => {
  test("activates on a workspace with .http files and registers its commands", async () => {
    await api();
    const commands = await vscode.commands.getCommands(true);
    for (const c of ["apic.showVersion", "apic.openInstallPage"]) {
      assert.ok(commands.includes(c), `command ${c} missing`);
    }
  });

  test("finds the project root above a request file", async () => {
    const { projectRoot } = await api();
    const folder = vscode.workspace.workspaceFolders?.[0];
    assert.ok(folder, "the fixture workspace should be open");
    const file = vscode.Uri.file(path.join(folder.uri.fsPath, "nested", "deep.http"));
    assert.strictEqual(projectRoot(file), folder.uri.fsPath);
    assert.strictEqual(projectRoot(undefined), folder.uri.fsPath);
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
    const res = await apic.json<{ requests: { id: string }[] }>(["list"], {
      project: vscode.workspace.workspaceFolders![0].uri.fsPath,
    });
    assert.strictEqual(res.code, 0, res.stderr);
    assert.ok(res.value?.requests.some((r) => r.id === "ping"), JSON.stringify(res.value));
  });
});
