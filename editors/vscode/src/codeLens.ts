// Run, Describe and Copy as curl above every request, and Run file as
// flow at the top. Positions come from `apic list --json` for the
// project, cached until a save or an environment change; a file the
// project cannot parse falls back to a scan of its text so the lenses
// still appear.
import * as vscode from "vscode";
import * as path from "node:path";
import type { Apic } from "./apic";
import { normalizeRelative, positionsFromList, requestAt, scanRequests, type RequestPosition } from "./lens";
import { projectRoot } from "./project";
import type { ListOutput } from "./types";

export class ApicCodeLens implements vscode.CodeLensProvider, vscode.Disposable {
  private readonly changed = new vscode.EventEmitter<void>();
  readonly onDidChangeCodeLenses = this.changed.event;
  private readonly lists = new Map<string, Promise<ListOutput | undefined>>();
  private readonly disposables: vscode.Disposable[] = [];

  constructor(private readonly apic: Apic) {
    this.disposables.push(
      this.changed,
      vscode.workspace.onDidSaveTextDocument((doc) => {
        const root = projectRoot(doc.uri);
        if (root) {
          this.invalidate(root);
        }
      }),
      vscode.workspace.onDidChangeConfiguration((e) => {
        if (e.affectsConfiguration("apic")) {
          this.lists.clear();
          this.changed.fire();
        }
      }),
    );
  }

  dispose(): void {
    vscode.Disposable.from(...this.disposables).dispose();
  }

  /** Forgets the cached request list of a project and redraws its lenses. */
  invalidate(root: string): void {
    this.lists.delete(root);
    this.changed.fire();
  }

  private list(root: string): Promise<ListOutput | undefined> {
    let p = this.lists.get(root);
    if (!p) {
      p = this.apic
        .json<ListOutput>(["list"], { project: root })
        .then((res) => res.value)
        .catch(() => undefined);
      this.lists.set(root, p);
    }
    return p;
  }

  /** The requests in a document, from apic when it can parse the project, else from the text. */
  async positions(document: vscode.TextDocument): Promise<{ root: string; file: string; positions: RequestPosition[] } | undefined> {
    const root = projectRoot(document.uri);
    if (!root || document.uri.scheme !== "file") {
      return undefined;
    }
    const file = normalizeRelative(path.relative(root, document.uri.fsPath));
    const list = document.isDirty ? undefined : await this.list(root);
    let positions = list ? positionsFromList(list, file) : [];
    if (positions.length === 0) {
      positions = scanRequests(document.getText(), file);
    }
    return { root, file, positions };
  }

  /** The request whose block holds the cursor. */
  async requestUnderCursor(editor: vscode.TextEditor): Promise<{ root: string; file: string; position: RequestPosition } | undefined> {
    const found = await this.positions(editor.document);
    if (!found) {
      return undefined;
    }
    const position = requestAt(found.positions, editor.selection.active.line + 1, editor.document.getText());
    return position ? { root: found.root, file: found.file, position } : undefined;
  }

  async provideCodeLenses(document: vscode.TextDocument): Promise<vscode.CodeLens[]> {
    if (!vscode.workspace.getConfiguration("apic", document.uri).get<boolean>("codeLens.enable", true)) {
      return [];
    }
    const found = await this.positions(document);
    if (!found || found.positions.length === 0) {
      return [];
    }
    const { root, file, positions } = found;
    const lenses: vscode.CodeLens[] = [];
    if (positions.length > 1) {
      lenses.push(new vscode.CodeLens(new vscode.Range(0, 0, 0, 0), { title: "$(run-all) Run file as flow", command: "apic.runFile", arguments: [root, file] }));
    }
    for (const p of positions) {
      const line = Math.max(0, Math.min(p.line - 1, document.lineCount - 1));
      const range = new vscode.Range(line, 0, line, 0);
      lenses.push(
        new vscode.CodeLens(range, { title: "$(play) Run", command: "apic.runRequest", arguments: [root, p.target], tooltip: `apic run ${p.target}` }),
        new vscode.CodeLens(range, { title: "Describe", command: "apic.describeRequest", arguments: [root, p.target], tooltip: `apic describe ${p.target}` }),
        new vscode.CodeLens(range, { title: "Copy as curl", command: "apic.copyCurl", arguments: [root, p.target], tooltip: `apic curl ${p.target}` }),
      );
    }
    return lenses;
  }
}
