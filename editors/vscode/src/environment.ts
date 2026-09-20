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
  /** apic.yaml's own default per project, from `apic env --json`, so the status bar can name it. */
  private readonly defaults = new Map<string, Promise<string | undefined>>();
  /** The project the status bar shows, so a slow `apic env` for another one cannot overwrite it. */
  private shownRoot: string | undefined;
  /** Fires with the project root whose environment changed. */
  readonly onDidChange = this.changed.event;

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly apic: Apic,
  ) {
    this.status = vscode.window.createStatusBarItem("apic.env", vscode.StatusBarAlignment.Left, 50);
    this.status.name = "apic environment";
    this.status.command = "apic.selectEnvironment";
    context.subscriptions.push(this.status, this.changed);
  }

  /** Forgets what `apic env` said about a project's default. */
  invalidate(root: string): void {
    this.defaults.delete(root);
  }

  /** The environment apic would use for a project: the picked one, else apic.yaml's, else undefined. */
  async effective(root: string): Promise<string | undefined> {
    return this.current(root) ?? (await this.projectDefault(root));
  }

  private projectDefault(root: string): Promise<string | undefined> {
    let p = this.defaults.get(root);
    if (!p) {
      p = this.apic
        .json<EnvOutput>(["env"], { project: root })
        .then((res) => res.value?.current || undefined)
        .catch(() => undefined);
      this.defaults.set(root, p);
    }
    return p;
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

  /** Shows the environment in effect for the project of the active editor. */
  refreshStatus(root: string | undefined): void {
    this.shownRoot = root;
    if (!root) {
      this.status.hide();
      return;
    }
    const picked = this.current(root);
    this.status.text = `$(globe) ${picked ?? "env"}`;
    this.status.tooltip = picked ? `apic runs in the ${picked} environment (click to change)` : "apic runs in the project's default environment (click to change)";
    this.status.show();
    if (!picked) {
      void this.projectDefault(root).then((def) => {
        if (this.shownRoot === root && this.current(root) === undefined) {
          this.status.text = `$(globe) ${def ?? "env: none"}`;
          this.status.tooltip = def ? `apic runs in ${def}, the project's default environment (click to change)` : "This project has no environments (click to pick one once it has)";
        }
      });
    }
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
