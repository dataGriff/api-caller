// The apic extension: a thin client over the apic binary's --json contract.
// Binary discovery and a version check, diagnostics from `apic validate`,
// CodeLens to run, describe and copy a request, a response panel, and an
// environment picker. Views and completions land on top of this.
import * as vscode from "vscode";
import * as fs from "node:fs";
import * as path from "node:path";
import { Apic, compareVersions, INSTALL_URL, MIN_VERSION, NotInstalledError } from "./apic";
import { ApicCodeActions } from "./codeActions";
import { ApicCodeLens } from "./codeLens";
import { Decorations } from "./decorations";
import { Diagnostics, triggersValidation, WATCH_GLOB } from "./diagnostics";
import { Environments } from "./environment";
import { Formatter } from "./formatter";
import { projectRoot } from "./project";
import { RequestsView } from "./requestsView";
import { ResponsePanel } from "./responsePanel";
import { Runner } from "./runner";
import { SessionView } from "./sessionView";
import type { RunResult } from "./types";
import { directivesFromGrammar } from "./validate";

/** What activate returns, for tests and for later features to share. */
export interface ApicApi {
  apic: Apic;
  projectRoot: typeof projectRoot;
  /** Validates every project in the workspace now and resolves when the diagnostics are published. */
  validateNow: () => Promise<void>;
  /** The results of the last run the panel showed. */
  lastResults: () => RunResult[];
  diagnostics: Diagnostics;
  environments: Environments;
  requestsView: RequestsView;
  sessionView: SessionView;
}

/** Request files, whatever language an installed extension gives them. */
const requestFiles: vscode.DocumentSelector = [
  { scheme: "file", pattern: "**/*.http" },
  { scheme: "file", pattern: "**/*.rest" },
];

export async function activate(context: vscode.ExtensionContext): Promise<ApicApi> {
  const output = vscode.window.createOutputChannel("apic");
  const apic = new Apic(output);
  const envs = new Environments(context, apic);
  const diagnostics = new Diagnostics(apic);
  const panel = new ResponsePanel(context);
  const decorations = new Decorations();
  const runner = new Runner(apic, envs, panel, decorations, output);
  const lens = new ApicCodeLens(apic);
  const requestsView = new RequestsView(apic, envs);
  const sessionView = new SessionView(apic, envs);
  context.subscriptions.push(output, diagnostics, panel, decorations, lens, requestsView, sessionView);

  let known: string[] | undefined;
  const knownDirectives = (): readonly string[] => {
    if (!known) {
      try {
        known = directivesFromGrammar(fs.readFileSync(vscode.Uri.joinPath(context.extensionUri, "syntaxes", "apic-directives.injection.json").fsPath, "utf8"));
      } catch {
        known = [];
      }
    }
    return known;
  };

  // Request and config files changing on disk, whether saved here or
  // written by git, apic import or anything else: the request list and
  // the diagnostics follow.
  const watcher = vscode.workspace.createFileSystemWatcher(WATCH_GLOB);
  const onDisk = (uri: vscode.Uri) => {
    lens.fileChanged(uri);
    diagnostics.changed(uri);
    if (uri.scheme === "file" && triggersValidation(uri)) {
      const root = projectRoot(uri);
      if (root) {
        envs.invalidate(root);
        requestsView.refresh(root);
        sessionView.refresh();
      }
    }
  };
  context.subscriptions.push(
    watcher,
    watcher.onDidChange(onDisk),
    watcher.onDidCreate(onDisk),
    watcher.onDidDelete(onDisk),
    vscode.workspace.onDidSaveTextDocument((doc) => diagnostics.changed(doc.uri)),
    vscode.languages.registerCodeLensProvider(requestFiles, lens),
    vscode.languages.registerDocumentFormattingEditProvider(requestFiles, new Formatter(apic)),
    vscode.languages.registerCodeActionsProvider(requestFiles, new ApicCodeActions(knownDirectives, (doc) => lens.names(doc)), ApicCodeActions.metadata),
    envs.onDidChange((root) => {
      lens.invalidate(root);
      requestsView.refresh(root);
      sessionView.refresh();
      if (diagnostics.auto()) {
        diagnostics.schedule(root);
      }
    }),
    runner.onDidRun(() => sessionView.refresh()),
    vscode.window.registerTreeDataProvider("apic.requests", requestsView),
    vscode.window.registerTreeDataProvider("apic.session", sessionView),
    vscode.window.onDidChangeActiveTextEditor((editor) => envs.refreshStatus(editor ? projectRoot(editor.document.uri) : undefined)),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("apic.path")) {
        apic.reset();
        void diagnostics.validateWorkspace();
      }
    }),
  );

  const activeRoot = (): string | undefined => projectRoot(vscode.window.activeTextEditor?.document.uri);

  context.subscriptions.push(
    vscode.commands.registerCommand("apic.showVersion", async () => {
      try {
        const info = await apic.version();
        const bin = await apic.binary();
        const root = activeRoot();
        void vscode.window.showInformationMessage(`apic ${info.version} (${info.os}/${info.arch}) at ${bin}${root ? ` · project ${root}` : ""}`);
      } catch (err) {
        await reportMissing(err);
      }
    }),
    vscode.commands.registerCommand("apic.openInstallPage", () => vscode.env.openExternal(vscode.Uri.parse(INSTALL_URL))),
    vscode.commands.registerCommand("apic.runRequest", (root?: string, target?: string) => withTarget(root, target, (r, t) => runner.run(r, [t]))),
    vscode.commands.registerCommand("apic.describeRequest", (root?: string, target?: string) => withTarget(root, target, (r, t) => runner.describe(r, t))),
    vscode.commands.registerCommand("apic.copyCurl", (root?: string, target?: string) => withTarget(root, target, (r, t) => runner.copyCurl(r, t, false))),
    vscode.commands.registerCommand("apic.copyCurlRedacted", (root?: string, target?: string) => withTarget(root, target, (r, t) => runner.copyCurl(r, t, true))),
    vscode.commands.registerCommand("apic.runFile", async (root?: string, file?: string) => {
      const editor = vscode.window.activeTextEditor;
      if (!root || !file) {
        const found = editor ? await lens.positions(editor.document) : undefined;
        if (!found) {
          void vscode.window.showInformationMessage("Open a .http file to run it as a flow.");
          return;
        }
        root = found.root;
        file = found.file;
      }
      await guarded(() => runner.run(root!, [file!], file));
    }),
    vscode.commands.registerCommand("apic.showLastResponse", () => panel.reveal()),
    vscode.commands.registerCommand("apic.selectEnvironment", async () => {
      const root = activeRoot() ?? vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
      if (!root) {
        void vscode.window.showInformationMessage("Open a project first.");
        return;
      }
      await guarded(() => envs.pick(root));
    }),
    vscode.commands.registerCommand("apic.pickEnvironment", () => vscode.commands.executeCommand("apic.selectEnvironment")),
    vscode.commands.registerCommand("apic.refresh", () => {
      requestsView.refresh();
      sessionView.refresh();
      for (const root of new Set([activeRoot(), projectRoot(undefined)])) {
        if (root) {
          envs.invalidate(root);
          lens.invalidate(root);
        }
      }
      envs.refreshStatus(activeRoot());
    }),
    vscode.commands.registerCommand("apic.clearSession", () => guarded(() => sessionView.clear(false))),
    vscode.commands.registerCommand("apic.clearAllSessions", () => guarded(() => sessionView.clear(true))),
    vscode.commands.registerCommand("apic.openRequest", async (root: string, file: string, line: number) => {
      const doc = await vscode.workspace.openTextDocument(vscode.Uri.file(path.join(root, file)));
      const pos = new vscode.Position(Math.max(0, Math.min(line - 1, doc.lineCount - 1)), 0);
      await vscode.window.showTextDocument(doc, { selection: new vscode.Range(pos, pos), preview: true });
    }),
    // The view's inline actions pass the node; the lens passes root and target.
    vscode.commands.registerCommand("apic.runNode", (node: { root: string; entry: { id: string } }) => guarded(() => runner.run(node.root, [node.entry.id]))),
    vscode.commands.registerCommand("apic.describeNode", (node: { root: string; entry: { id: string } }) => guarded(() => runner.describe(node.root, node.entry.id))),
    vscode.commands.registerCommand("apic.copyCurlNode", (node: { root: string; entry: { id: string } }) => guarded(() => runner.copyCurl(node.root, node.entry.id, false))),
    vscode.commands.registerCommand("apic.runFileNode", (node: { root: string; file: string }) => guarded(() => runner.run(node.root, [node.file], node.file))),
    vscode.commands.registerCommand("apic.validate", async (uri?: vscode.Uri) => {
      const root = uri ? projectRoot(uri) : activeRoot();
      await guarded(() => (root ? diagnostics.validate(root) : diagnostics.validateWorkspace(true)));
    }),
  );

  /** Resolves the request to act on: the lens arguments, else the request under the cursor. */
  async function withTarget<T>(root: string | undefined, target: string | undefined, fn: (root: string, target: string) => Promise<T>): Promise<T | undefined> {
    if (!root || !target) {
      const editor = vscode.window.activeTextEditor;
      const found = editor ? await lens.requestUnderCursor(editor) : undefined;
      if (!found) {
        void vscode.window.showInformationMessage("Put the cursor on a request in a .http file first.");
        return undefined;
      }
      root = found.root;
      target = found.position.target;
    }
    return guarded(() => fn(root!, target!));
  }

  /** Runs an apic-backed action; a missing binary gets the install prompt rather than a bare command failure. */
  async function guarded<T>(fn: () => Promise<T>): Promise<T | undefined> {
    try {
      return await fn();
    } catch (err) {
      await reportMissing(err);
      return undefined;
    }
  }

  envs.refreshStatus(activeRoot());

  // Check the binary once, quietly: a missing or old apic is reported with
  // a way to fix it, and nothing else in the extension will work until it
  // is, so this is the one prompt worth showing on activation. Then
  // validate what is open, so the Problems panel is populated from the
  // start.
  void checkBinary(apic).then(() => diagnostics.validateWorkspace());

  return {
    apic,
    projectRoot,
    validateNow: () => diagnostics.validateWorkspace(),
    lastResults: () => panel.last(),
    diagnostics,
    environments: envs,
    requestsView,
    sessionView,
  };
}

export function deactivate(): void {
  // Nothing to release: everything is in context.subscriptions.
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
