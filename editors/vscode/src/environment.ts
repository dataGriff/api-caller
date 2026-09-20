// The environment a project's commands run in: apic's own default (env:
// in apic.yaml) unless the user picked another one, remembered per
// project in the workspace state and shown in the status bar.
import * as vscode from "vscode";
import type { Apic } from "./apic";
import type { EnvOutput } from "./types";

interface EnvItem extends vscode.QuickPickItem {
  /** The environment to pass as --env, or undefined for the project's default. */
  env: string | undefined;
}

export class Environments {
  private readonly status: vscode.StatusBarItem;
  private readonly changed = new vscode.EventEmitter<string>();
  /** Fires with the project root whose environment changed. */
  readonly onDidChange = this.changed.event;

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly apic: Apic,
  ) {
    this.status = vscode.window.createStatusBarItem("apic.env", vscode.StatusBarAlignment.Left, 50);
    this.status.name = "apic environment";
    this.status.command = "apic.pickEnvironment";
    context.subscriptions.push(this.status, this.changed);
  }

  private key(root: string): string {
    return `apic.env:${root}`;
  }

  /** The environment picked for a project, or undefined for apic's default. */
  current(root: string): string | undefined {
    return this.context.workspaceState.get<string>(this.key(root));
  }

  /** The `--env` arguments for a project's commands, empty for the default. */
  args(root: string): string[] {
    const env = this.current(root);
    return env ? ["--env", env] : [];
  }

  /** Shows the picked environment for the project of the active editor. */
  refreshStatus(root: string | undefined): void {
    if (!root) {
      this.status.hide();
      return;
    }
    const env = this.current(root);
    this.status.text = `$(globe) ${env ?? "env: default"}`;
    this.status.tooltip = env ? `apic runs in the ${env} environment (click to change)` : "apic runs in the project's default environment (click to change)";
    this.status.show();
  }

  /** Asks the user to pick an environment from the project's env files. */
  async pick(root: string): Promise<void> {
    const res = await this.apic.json<EnvOutput>(["env"], { project: root });
    const names = res.value?.environments ?? [];
    const current = this.current(root);
    // The default is its own item, not a name, so an environment that is
    // literally called "default" is still pickable.
    const items: EnvItem[] = [
      { label: "$(circle-slash) Project default", description: res.value?.current ? `apic.yaml says ${res.value.current}` : "no --env", picked: !current, env: undefined },
      ...names.map((n) => ({ label: n, description: n === res.value?.current ? "the project default" : "", picked: n === current, env: n })),
    ];
    if (names.length === 0) {
      void vscode.window.showInformationMessage("This project has no environments in http-client.env.json; commands run without --env.");
    }
    const choice = await vscode.window.showQuickPick(items, { placeHolder: "Environment for apic commands in this project" });
    if (!choice) {
      return;
    }
    await this.context.workspaceState.update(this.key(root), choice.env);
    this.refreshStatus(root);
    this.changed.fire(root);
  }
}
