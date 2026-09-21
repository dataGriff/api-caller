// "used by N scenarios" above every `# @step` line of a request file,
// counted from the features the Test Explorer parsed; a click opens the
// step in its feature, or asks which when there are several.
import * as vscode from "vscode";
import { stepUsages, type StepUsage } from "./featureParser";
import { projectRoot } from "./project";
import type { TestExplorer } from "./testController";

const stepLine = /^\s*(?:#|\/\/)\s*@step\s+(.+?)\s*$/;

export class StepCodeLens implements vscode.CodeLensProvider, vscode.Disposable {
  private readonly changed = new vscode.EventEmitter<void>();
  readonly onDidChangeCodeLenses = this.changed.event;
  private readonly disposables: vscode.Disposable[] = [];

  constructor(private readonly tests: TestExplorer) {
    this.disposables.push(
      this.changed,
      tests.onDidChangeFeatures(() => this.changed.fire()),
    );
  }

  dispose(): void {
    vscode.Disposable.from(...this.disposables).dispose();
  }

  provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
    if (!vscode.workspace.getConfiguration("apic", document.uri).get<boolean>("codeLens.enable", true)) {
      return [];
    }
    const root = projectRoot(document.uri);
    if (!root) {
      return [];
    }
    const features = this.tests.featuresIn(root);
    const lenses: vscode.CodeLens[] = [];
    for (let i = 0; i < document.lineCount; i++) {
      const m = stepLine.exec(document.lineAt(i).text);
      if (!m) {
        continue;
      }
      const { count, locations } = stepUsages(features, m[1]);
      const title = count === 0 ? "not used by any scenario" : `used by ${count} scenario${count === 1 ? "" : "s"}`;
      lenses.push(new vscode.CodeLens(new vscode.Range(i, 0, i, 0), { title, command: "apic.revealStepUsages", arguments: [locations], tooltip: count === 0 ? "No step in the project's features matches this phrase" : "Open the feature" }));
    }
    return lenses;
  }
}

/** Opens the one place a phrase is used, or asks which. */
export async function revealStepUsages(usages: StepUsage[]): Promise<void> {
  if (usages.length === 0) {
    void vscode.window.showInformationMessage("No scenario uses this phrase yet. Write one in a .feature file under the project's test paths.");
    return;
  }
  let target = usages[0];
  if (usages.length > 1) {
    const choice = await vscode.window.showQuickPick(
      usages.map((u) => ({ label: u.label, description: `${vscode.workspace.asRelativePath(u.uri)}:${u.line}`, usage: u })),
      { placeHolder: "Where this phrase is used" },
    );
    if (!choice) {
      return;
    }
    target = choice.usage;
  }
  const doc = await vscode.workspace.openTextDocument(vscode.Uri.file(target.uri));
  const pos = new vscode.Position(Math.max(0, Math.min(target.line - 1, doc.lineCount - 1)), 0);
  await vscode.window.showTextDocument(doc, { selection: new vscode.Range(pos, pos), preview: true });
}
