// The `✓ 200 · 12 ms` that appears at the end of a request line after a
// run, cleared when the file changes.
import * as vscode from "vscode";
import type { RunResult } from "./types";

export class Decorations implements vscode.Disposable {
  private readonly pass: vscode.TextEditorDecorationType;
  private readonly fail: vscode.TextEditorDecorationType;
  /** Per file: the decorated lines and their labels, so a newly opened editor gets them too. */
  private readonly byFile = new Map<string, { line: number; ok: boolean; label: string }[]>();
  private readonly disposables: vscode.Disposable[] = [];

  constructor() {
    const make = (color: string) =>
      vscode.window.createTextEditorDecorationType({
        after: { margin: "0 0 0 1.5em", color: new vscode.ThemeColor(color), fontStyle: "italic" },
        rangeBehavior: vscode.DecorationRangeBehavior.ClosedClosed,
      });
    this.pass = make("testing.iconPassed");
    this.fail = make("testing.iconFailed");
    this.disposables.push(
      this.pass,
      this.fail,
      vscode.workspace.onDidChangeTextDocument((e) => {
        if (this.byFile.delete(e.document.uri.toString())) {
          this.apply(e.document.uri);
        }
      }),
      vscode.window.onDidChangeVisibleTextEditors((editors) => editors.forEach((ed) => this.apply(ed.document.uri))),
    );
  }

  dispose(): void {
    vscode.Disposable.from(...this.disposables).dispose();
  }

  /** Records the outcome of a run and shows it in every editor of the files involved. */
  show(root: string, results: RunResult[]): void {
    const touched = new Set<string>();
    for (const r of results) {
      const uri = vscode.Uri.joinPath(vscode.Uri.file(root), r.request.file);
      const key = uri.toString();
      const list = this.byFile.get(key) ?? [];
      const label = r.response
        ? `${r.ok ? "✓" : "✗"} ${r.response.status} · ${r.response.duration_ms} ms${r.attempts && r.attempts > 1 ? ` · ${r.attempts} attempts` : ""}`
        : "✗ not sent";
      const line = Math.max(0, r.request.line - 1);
      const idx = list.findIndex((d) => d.line === line);
      const entry = { line, ok: r.ok, label };
      if (idx >= 0) {
        list[idx] = entry;
      } else {
        list.push(entry);
      }
      this.byFile.set(key, list);
      touched.add(key);
    }
    for (const key of touched) {
      this.apply(vscode.Uri.parse(key));
    }
  }

  private apply(uri: vscode.Uri): void {
    const entries = this.byFile.get(uri.toString()) ?? [];
    for (const editor of vscode.window.visibleTextEditors) {
      if (editor.document.uri.toString() !== uri.toString()) {
        continue;
      }
      const options = (ok: boolean): vscode.DecorationOptions[] =>
        entries
          .filter((e) => e.ok === ok && e.line < editor.document.lineCount)
          .map((e) => {
            const end = editor.document.lineAt(e.line).range.end;
            return { range: new vscode.Range(end, end), renderOptions: { after: { contentText: e.label } } };
          });
      editor.setDecorations(this.pass, options(true));
      editor.setDecorations(this.fail, options(false));
    }
  }
}
