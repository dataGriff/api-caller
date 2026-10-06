// The language client: with apic.languageServer.enable on, diagnostics,
// completion and hover come from `apic lsp` instead of the extension's own
// providers, so problems show as you type rather than on save. The code
// lenses, the formatter, the response panel, the views and the Test
// Explorer stay the extension's.
import * as vscode from "vscode";
import { LanguageClient, type LanguageClientOptions, type ServerOptions } from "vscode-languageclient/node";
import { compareVersions, type Apic } from "./apic";

/** The first apic release with `apic lsp`. */
export const LSP_MIN_VERSION = "0.2.0";

/** Whether the setting asks for the language server. */
export function languageServerEnabled(): boolean {
  return vscode.workspace.getConfiguration("apic").get<boolean>("languageServer.enable", false);
}

/** What the server is told at start. */
export interface StartOptions {
  /** The environment picked per project root. */
  envs: Record<string, string>;
  /** apic.projectDir resolved per workspace folder: folder → project root. */
  projectRoots: Record<string, string>;
}

/** The initialisation options: the environments and roots, and to leave lenses and formatting to the extension. */
export function initializationOptions(o: StartOptions): Record<string, unknown> {
  return { envs: o.envs, projectRoots: o.projectRoots, codeLens: false, formatting: false };
}

/**
 * Starts `apic lsp` for the workspace's request files. Resolves to
 * undefined, after saying why, when the binary is missing, too old to
 * have a language server, or the server fails to start; the extension
 * then keeps its own providers.
 */
export async function startLanguageClient(apic: Apic, o: StartOptions, output: vscode.OutputChannel): Promise<LanguageClient | undefined> {
  let bin: string;
  try {
    bin = await apic.binary();
    const { version } = await apic.version();
    if (compareVersions(version, LSP_MIN_VERSION) < 0) {
      void vscode.window.showWarningMessage(`apic ${version} has no language server (it came in ${LSP_MIN_VERSION}); using the extension's own diagnostics, completion and hover.`);
      return undefined;
    }
  } catch (err) {
    output.appendLine(`apic lsp not started: ${String(err)}`);
    return undefined;
  }
  const folder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  const server: ServerOptions = { command: bin, args: ["lsp"], options: folder ? { cwd: folder } : undefined };
  const client: LanguageClientOptions = {
    documentSelector: [
      { scheme: "file", pattern: "**/*.http" },
      { scheme: "file", pattern: "**/*.rest" },
    ],
    initializationOptions: initializationOptions(o),
  };
  const lc = new LanguageClient("apic", "apic language server", server, client);
  try {
    await lc.start();
    return lc;
  } catch (err) {
    output.appendLine(`apic lsp failed to start: ${String(err)}`);
    void vscode.window.showWarningMessage("apic's language server did not start (see the apic output); using the extension's own diagnostics, completion and hover.");
    await lc.dispose().catch(() => undefined);
    return undefined;
  }
}

/** Tells a running server the environment picked for one project. */
export async function setEnvironment(lc: LanguageClient | undefined, root: string, env: string | undefined): Promise<void> {
  await lc?.sendNotification("workspace/didChangeConfiguration", { settings: { apic: { envs: { [root]: env ?? "" } } } });
}

/** Asks the server to check the workspace now; resolves once the diagnostics are published. */
export async function validateWith(lc: LanguageClient): Promise<void> {
  await lc.sendRequest("workspace/executeCommand", { command: "apic.lsp.validate", arguments: [] });
}
