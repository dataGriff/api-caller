// Quick fixes for the diagnostics apic reports: a typo in a directive, a
// request name used twice, a body file that does not exist. The known
// directives come from the shipped grammar and the taken names from
// `apic list`, so nothing here parses request files.
import * as vscode from "vscode";
import * as path from "node:path";
import { nearestDirective } from "./validate";

export class ApicCodeActions implements vscode.CodeActionProvider {
  static readonly metadata: vscode.CodeActionProviderMetadata = { providedCodeActionKinds: [vscode.CodeActionKind.QuickFix] };

  constructor(
    private readonly knownDirectives: () => readonly string[],
    /** The request names in the project a document belongs to. */
    private readonly namesIn: (document: vscode.TextDocument) => Promise<readonly string[]>,
  ) {}

  async provideCodeActions(document: vscode.TextDocument, _range: vscode.Range, context: vscode.CodeActionContext): Promise<vscode.CodeAction[]> {
    const actions: vscode.CodeAction[] = [];
    for (const d of context.diagnostics) {
      if (d.source !== "apic") {
        continue;
      }
      const code = typeof d.code === "object" ? String(d.code.value) : String(d.code ?? "");
      const text = document.getText(d.range);
      switch (code) {
        case "unknown-directive": {
          const name = text.replace(/^@/, "");
          const suggestion = nearestDirective(name, this.knownDirectives());
          if (suggestion) {
            const action = new vscode.CodeAction(`Change to @${suggestion}`, vscode.CodeActionKind.QuickFix);
            action.diagnostics = [d];
            action.isPreferred = true;
            action.edit = new vscode.WorkspaceEdit();
            action.edit.replace(document.uri, d.range, `@${suggestion}`);
            actions.push(action);
          }
          break;
        }
        case "duplicate-name": {
          const base = text.trim().replace(/-\d+$/, "");
          const taken = new Set(await this.namesIn(document));
          let n = 2;
          while (taken.has(`${base}-${n}`)) {
            n++;
          }
          const renamed = `${base}-${n}`;
          const action = new vscode.CodeAction(`Rename to ${renamed}`, vscode.CodeActionKind.QuickFix);
          action.diagnostics = [d];
          action.edit = new vscode.WorkspaceEdit();
          action.edit.replace(document.uri, d.range, renamed);
          actions.push(action);
          break;
        }
        case "missing-body-file": {
          const file = text.trim();
          if (file) {
            const target = vscode.Uri.file(path.resolve(path.dirname(document.uri.fsPath), file));
            const action = new vscode.CodeAction(`Create ${file}`, vscode.CodeActionKind.QuickFix);
            action.diagnostics = [d];
            action.isPreferred = true;
            action.edit = new vscode.WorkspaceEdit();
            action.edit.createFile(target, { ignoreIfExists: true });
            action.command = { command: "apic.validate", title: "Validate", arguments: [document.uri] };
            actions.push(action);
          }
          break;
        }
        default:
          break;
      }
    }
    return actions;
  }
}
