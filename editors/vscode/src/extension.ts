// The apic extension: a thin client over the apic binary's --json contract.
// This is the scaffold: binary discovery, a version check with an install
// prompt, an output channel, and one command. CodeLens, diagnostics, the
// response viewer and the views land on top of it.
import * as vscode from "vscode";
import { Apic, compareVersions, INSTALL_URL, MIN_VERSION, NotInstalledError } from "./apic";
import { projectRoot } from "./project";

/** What activate returns, for tests and for later features to share. */
export interface ApicApi {
  apic: Apic;
  projectRoot: typeof projectRoot;
}

export async function activate(context: vscode.ExtensionContext): Promise<ApicApi> {
  const output = vscode.window.createOutputChannel("apic");
  const apic = new Apic(output);
  context.subscriptions.push(output);

  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("apic.path")) {
        apic.reset();
      }
    }),
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("apic.showVersion", async () => {
      try {
        const info = await apic.version();
        const bin = await apic.binary();
        const root = projectRoot(vscode.window.activeTextEditor?.document.uri);
        void vscode.window.showInformationMessage(
          `apic ${info.version} (${info.os}/${info.arch}) at ${bin}${root ? ` · project ${root}` : ""}`,
        );
      } catch (err) {
        await reportMissing(err);
      }
    }),
    vscode.commands.registerCommand("apic.openInstallPage", () => vscode.env.openExternal(vscode.Uri.parse(INSTALL_URL))),
  );

  // Check the binary once, quietly: a missing or old apic is reported with
  // a way to fix it, and nothing else in the extension will work until it
  // is, so this is the one prompt worth showing on activation.
  void checkBinary(apic);

  return { apic, projectRoot };
}

export function deactivate(): void {
  // Nothing to release: the output channel and commands are in context.subscriptions.
}

async function checkBinary(apic: Apic): Promise<void> {
  try {
    const info = await apic.version();
    if (compareVersions(info.version, MIN_VERSION) < 0) {
      const choice = await vscode.window.showWarningMessage(
        `apic ${info.version} is older than this extension supports (${MIN_VERSION} or newer).`,
        "Install page",
      );
      if (choice) {
        await vscode.commands.executeCommand("apic.openInstallPage");
      }
    }
  } catch (err) {
    await reportMissing(err);
  }
}

async function reportMissing(err: unknown): Promise<void> {
  const message =
    err instanceof NotInstalledError
      ? `The apic binary was not found (${err.searched}). Install it, or point apic.path at it.`
      : `apic could not be run: ${err instanceof Error ? err.message : String(err)}`;
  const choice = await vscode.window.showErrorMessage(message, "Install page", "Open settings");
  if (choice === "Install page") {
    await vscode.commands.executeCommand("apic.openInstallPage");
  } else if (choice === "Open settings") {
    await vscode.commands.executeCommand("workbench.action.openSettings", "apic.path");
  }
}
