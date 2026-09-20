// The "Session" view: what `apic session` knows for the project's current
// environment: captured values (tokens as the description apic masks them
// into), and the cookies in the jar with their scope, never their values.
// Clearing goes through `apic session clear`, after a confirmation.
import * as vscode from "vscode";
import type { Apic } from "./apic";
import type { Environments } from "./environment";
import { projectRoot } from "./project";
import type { EnvOutput } from "./types";
import { sessionItems, type CookieInfo, type SessionItem } from "./views";

export class SessionView implements vscode.TreeDataProvider<SessionItem>, vscode.Disposable {
  private readonly changed = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.changed.event;
  private readonly disposables: vscode.Disposable[] = [];

  constructor(
    private readonly apic: Apic,
    private readonly envs: Environments,
  ) {
    this.disposables.push(this.changed, vscode.window.onDidChangeActiveTextEditor(() => this.refresh()));
  }

  dispose(): void {
    vscode.Disposable.from(...this.disposables).dispose();
  }

  root(): string | undefined {
    return projectRoot(vscode.window.activeTextEditor?.document.uri) ?? projectRoot(undefined);
  }

  refresh(): void {
    this.changed.fire();
  }

  getTreeItem(item: SessionItem): vscode.TreeItem {
    const t = new vscode.TreeItem(item.name, vscode.TreeItemCollapsibleState.None);
    t.description = item.shown;
    t.contextValue = item.kind;
    t.iconPath = new vscode.ThemeIcon(item.kind === "cookie" ? "symbol-key" : item.kind === "token" ? "key" : "symbol-variable");
    t.tooltip = item.kind === "cookie" ? "A cookie in the jar; values are never shown" : item.kind === "token" ? "A token cached by # @auth" : `${item.name} = ${item.shown}`;
    return t;
  }

  async getChildren(item?: SessionItem): Promise<SessionItem[]> {
    if (item) {
      return [];
    }
    const root = this.root();
    if (!root) {
      return [];
    }
    const env = await this.effectiveEnv(root);
    const [session, cookies] = await Promise.all([
      this.apic.json<Record<string, Record<string, string>>>(["session"], { project: root }).catch(() => undefined),
      this.apic.json<Record<string, CookieInfo[]>>(["session", "cookies"], { project: root }).catch(() => undefined),
    ]);
    const redact = vscode.workspace.getConfiguration("apic", vscode.Uri.file(root)).get<string[]>("run.extraArgs", []).includes("--redact");
    return sessionItems(session?.value?.[env] ?? {}, cookies?.value?.[env] ?? [], redact);
  }

  /** The environment apic would use: the picked one, else apic.yaml's, else "default". */
  async effectiveEnv(root: string): Promise<string> {
    const picked = this.envs.current(root);
    if (picked) {
      return picked;
    }
    const res = await this.apic.json<EnvOutput>(["env"], { project: root }).catch(() => undefined);
    return res?.value?.current || "default";
  }

  /** Runs `apic session clear` for the current environment, or every one, after asking. */
  async clear(all: boolean): Promise<void> {
    const root = this.root();
    if (!root) {
      void vscode.window.showInformationMessage("Open a project first.");
      return;
    }
    const env = await this.effectiveEnv(root);
    const what = all ? "every environment's captured values and cookies" : `the captured values and cookies of ${env}`;
    const choice = await vscode.window.showWarningMessage(`Forget ${what}?`, { modal: true }, "Clear");
    if (choice !== "Clear") {
      return;
    }
    const res = await this.apic.run(["session", "clear", ...(all ? ["--all"] : this.envs.args(root))], { project: root });
    if (res.code !== 0) {
      void vscode.window.showErrorMessage(`apic session clear failed: ${res.stderr.trim() || `exit ${res.code}`}`);
    }
    this.refresh();
  }
}
