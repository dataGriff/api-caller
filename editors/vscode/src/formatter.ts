// Format Document for .http files, through `apic fmt -`: the document's
// text goes to the binary's stdin and the canonical form comes back as
// one edit, so formatOnSave works with nothing else configured. A file
// apic cannot read is left alone with a warning, never a broken edit.
import * as vscode from "vscode";
import type { Apic } from "./apic";
import { fullDocumentEdit } from "./format";
import { projectRoot } from "./project";

export class Formatter implements vscode.DocumentFormattingEditProvider {
  constructor(private readonly apic: Apic) {}

  async provideDocumentFormattingEdits(document: vscode.TextDocument, _options: vscode.FormattingOptions, token: vscode.CancellationToken): Promise<vscode.TextEdit[]> {
    if (!vscode.workspace.getConfiguration("apic", document.uri).get<boolean>("format.enable", true)) {
      return [];
    }
    const text = document.getText();
    const controller = new AbortController();
    token.onCancellationRequested(() => controller.abort());
    const res = await this.apic.run(["fmt", "-"], { project: projectRoot(document.uri), input: text, signal: controller.signal });
    if (res.aborted) {
      return [];
    }
    if (res.code !== 0) {
      void vscode.window.showWarningMessage(`apic fmt could not format ${vscode.workspace.asRelativePath(document.uri)}: ${res.stderr.trim() || `exit ${res.code}`}`);
      return [];
    }
    const edit = fullDocumentEdit(text, res.stdout);
    if (!edit) {
      return [];
    }
    return [vscode.TextEdit.replace(new vscode.Range(document.positionAt(edit.start), document.positionAt(edit.end)), edit.newText)];
  }
}
