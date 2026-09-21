// Completions in request files, from complete.ts: directives after `# @`
// (bodies as snippets), variables inside `{{` from `apic env` and the
// session, selectors after `# @assert` and `# @capture x =`, the keys of
// the last response after `body.$.`, operators, auth types and request
// names after `# @ref`. What apic said is cached per project until a
// request or config file changes, the environment is switched or a run
// finishes.
import * as vscode from "vscode";
import type { Apic } from "./apic";
import type { ApicCodeLens } from "./codeLens";
import { authItems, contextAt, directiveItems, lastBodyFor, operatorItems, refItems, selectorItems, variableItems, type Item } from "./complete";
import type { Environments } from "./environment";
import { projectRoot } from "./project";
import type { RunResult } from "./types";

const kinds: Record<Item["kind"], vscode.CompletionItemKind> = {
  directive: vscode.CompletionItemKind.Keyword,
  variable: vscode.CompletionItemKind.Variable,
  selector: vscode.CompletionItemKind.Field,
  operator: vscode.CompletionItemKind.Operator,
  keyword: vscode.CompletionItemKind.Function,
  request: vscode.CompletionItemKind.Reference,
};

export class ApicCompletions implements vscode.CompletionItemProvider {
  static readonly triggers = ["@", "{", ".", "$", "="];
  private readonly sessions = new Map<string, Promise<Record<string, string>>>();

  constructor(
    private readonly apic: Apic,
    private readonly envs: Environments,
    private readonly lens: ApicCodeLens,
    private readonly knownDirectives: () => readonly string[],
    /** The results of the last run, for `body.$.` paths. */
    private readonly lastResults: () => readonly RunResult[],
  ) {}

  /** Forgets the session of a project (or every one). */
  invalidate(root?: string): void {
    if (root) {
      this.sessions.delete(root);
    } else {
      this.sessions.clear();
    }
  }

  async provideCompletionItems(document: vscode.TextDocument, position: vscode.Position): Promise<vscode.CompletionItem[] | undefined> {
    const line = document.lineAt(position.line).text;
    const ctx = contextAt(line, position.character, line.slice(position.character));
    if (!ctx) {
      return undefined;
    }
    const root = projectRoot(document.uri);
    let items: Item[];
    switch (ctx.kind) {
      case "directive":
        items = directiveItems(this.knownDirectives());
        break;
      case "variable": {
        const [env, session, found] = await Promise.all([root ? this.envs.variables(root) : [], root ? this.session(root) : {}, this.lens.positions(document)]);
        const requests = (found?.positions ?? []).map((p) => p.name).filter((n): n is string => Boolean(n));
        items = variableItems({ env, session, requests }, ctx.closed);
        break;
      }
      case "selector": {
        const under = await this.lens.requestAtPosition(document, position);
        items = selectorItems(ctx.prefix, lastBodyFor(this.lastResults(), under?.position.name));
        break;
      }
      case "operator":
        items = operatorItems();
        break;
      case "auth":
        items = authItems();
        break;
      case "ref":
        items = refItems(await this.lens.names(document));
        break;
    }
    const range = new vscode.Range(position.line, ctx.start, position.line, position.character);
    return items.map((it) => {
      const c = new vscode.CompletionItem(it.label, kinds[it.kind]);
      c.insertText = it.snippet ? new vscode.SnippetString(it.insert) : it.insert;
      c.range = range;
      c.detail = it.detail;
      c.documentation = it.documentation ? new vscode.MarkdownString(it.documentation) : undefined;
      c.sortText = it.sortText;
      c.filterText = it.filterText;
      return c;
    });
  }

  /** The session's captured values for the project's environment in effect. */
  private session(root: string): Promise<Record<string, string>> {
    let p = this.sessions.get(root);
    if (!p) {
      p = (async () => {
        const env = (await this.envs.effective(root)) ?? "default";
        const res = await this.apic.json<Record<string, Record<string, string>>>(["session"], { project: root }).catch(() => undefined);
        return res?.value?.[env] ?? {};
      })();
      this.sessions.set(root, p);
    }
    return p;
  }
}
