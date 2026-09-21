// Hover on `{{name}}`: the value, its source and, for a missing one, the
// request that captures it, from `apic describe` of the request the
// cursor is in (cached until the project changes) or `apic env` outside
// one.
import * as vscode from "vscode";
import type { Apic } from "./apic";
import type { ApicCodeLens } from "./codeLens";
import type { Environments } from "./environment";
import { hoverText, placeholderAt } from "./hoverText";
import { projectRoot } from "./project";
import type { Description } from "./types";

export class ApicHover implements vscode.HoverProvider {
  private readonly described = new Map<string, Promise<Description | undefined>>();

  constructor(
    private readonly apic: Apic,
    private readonly envs: Environments,
    private readonly lens: ApicCodeLens,
  ) {}

  /** Forgets what `apic describe` said for a project (or every one). */
  invalidate(root?: string): void {
    for (const key of [...this.described.keys()]) {
      if (!root || key.startsWith(`${root}\0`)) {
        this.described.delete(key);
      }
    }
  }

  async provideHover(document: vscode.TextDocument, position: vscode.Position): Promise<vscode.Hover | undefined> {
    const line = document.lineAt(position.line).text;
    const ph = placeholderAt(line, position.character);
    if (!ph) {
      return undefined;
    }
    const range = new vscode.Range(position.line, ph.start, position.line, ph.end);
    const root = projectRoot(document.uri);
    const under = root ? await this.lens.requestAtPosition(document, position) : undefined;
    const description = under ? await this.describe(root!, under.position.target) : undefined;
    const env = root && !description ? await this.envs.variables(root) : undefined;
    const text = hoverText(ph.name, { description, env });
    return text ? new vscode.Hover(new vscode.MarkdownString(text), range) : undefined;
  }

  private describe(root: string, target: string): Promise<Description | undefined> {
    const key = `${root}\0${target}\0${this.envs.current(root) ?? ""}`;
    let p = this.described.get(key);
    if (!p) {
      p = this.apic
        .json<Description>(["describe", target, ...this.envs.args(root)], { project: root })
        .then((res) => res.value)
        .catch(() => undefined);
      this.described.set(key, p);
    }
    return p;
  }
}
