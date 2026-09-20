// The "Requests" view: every request of the project grouped by file, from
// `apic list --json`, with a ready/not-ready icon from `apic describe`
// and Run, Describe and Copy as curl as inline actions. Refreshed when a
// request or config file changes on disk and when the environment
// changes.
import * as vscode from "vscode";
import * as path from "node:path";
import type { Apic } from "./apic";
import type { Environments } from "./environment";
import { projectRoot } from "./project";
import type { Description, ListEntry, ListOutput } from "./types";
import { groupByFile, requestDetail, requestLabel, targetOf } from "./views";

export type RequestNode = { kind: "file"; root: string; file: string; requests: ListEntry[] } | { kind: "request"; root: string; entry: ListEntry; ready: boolean | undefined };

export class RequestsView implements vscode.TreeDataProvider<RequestNode>, vscode.Disposable {
  private readonly changed = new vscode.EventEmitter<RequestNode | undefined>();
  readonly onDidChangeTreeData = this.changed.event;
  private readonly lists = new Map<string, Promise<ListOutput | undefined>>();
  private readonly readiness = new Map<string, Promise<Map<string, boolean>>>();
  private readonly disposables: vscode.Disposable[] = [];
  private shownRoot: string | undefined;

  constructor(
    private readonly apic: Apic,
    private readonly envs: Environments,
  ) {
    this.disposables.push(
      this.changed,
      // Switching editors within one project changes nothing the view
      // shows; only a different project redraws it, and from the caches.
      vscode.window.onDidChangeActiveTextEditor(() => this.rootChanged()),
      vscode.workspace.onDidChangeWorkspaceFolders(() => this.refresh()),
    );
  }

  private rootChanged(): void {
    const root = this.root();
    if (root !== this.shownRoot) {
      this.shownRoot = root;
      this.changed.fire(undefined);
    }
  }

  dispose(): void {
    vscode.Disposable.from(...this.disposables).dispose();
  }

  /** The project the view shows: the active file's, else the first workspace folder's. */
  root(): string | undefined {
    return projectRoot(vscode.window.activeTextEditor?.document.uri) ?? projectRoot(undefined);
  }

  /** Forgets everything cached and redraws. */
  refresh(root?: string): void {
    if (root) {
      this.lists.delete(root);
      for (const key of [...this.readiness.keys()]) {
        if (key.startsWith(root + "|")) {
          this.readiness.delete(key);
        }
      }
    } else {
      this.lists.clear();
      this.readiness.clear();
    }
    this.changed.fire(undefined);
  }

  getTreeItem(node: RequestNode): vscode.TreeItem {
    if (node.kind === "file") {
      const item = new vscode.TreeItem(node.file, vscode.TreeItemCollapsibleState.Expanded);
      item.contextValue = "file";
      item.iconPath = vscode.ThemeIcon.File;
      item.resourceUri = vscode.Uri.file(path.join(node.root, node.file));
      item.description = `${node.requests.length} request${node.requests.length === 1 ? "" : "s"}`;
      item.tooltip = `apic run ${node.file}`;
      return item;
    }
    const r = node.entry;
    const item = new vscode.TreeItem(requestLabel(r), vscode.TreeItemCollapsibleState.None);
    item.id = `${node.root}|${r.id}`;
    item.contextValue = "request";
    item.description = requestDetail(r);
    item.tooltip = new vscode.MarkdownString(`**${r.method}** \`${r.url}\`\n\n${r.file}:${r.line}${node.ready === false ? "\n\nNot ready: a variable is missing (Describe says which)." : ""}`);
    item.iconPath =
      node.ready === undefined
        ? new vscode.ThemeIcon("circle-large-outline")
        : node.ready
          ? new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed"))
          : new vscode.ThemeIcon("circle-outline", new vscode.ThemeColor("descriptionForeground"));
    item.command = { command: "apic.openRequest", title: "Open in editor", arguments: [node.root, r.file, r.line] };
    return item;
  }

  async getChildren(node?: RequestNode): Promise<RequestNode[]> {
    if (!node) {
      const root = this.root();
      this.shownRoot = root;
      if (!root) {
        return [];
      }
      const list = await this.list(root);
      if (!list) {
        return [];
      }
      return groupByFile(list).map((g) => ({ kind: "file" as const, root, file: g.file, requests: g.requests }));
    }
    if (node.kind === "file") {
      const ready = await this.readinessOf(node.root, node.requests);
      return node.requests.map((entry) => ({ kind: "request" as const, root: node.root, entry, ready: ready.get(entry.id) }));
    }
    return [];
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

  /** Readiness per request id, from one `describe` each, cached per project and environment. */
  private readinessOf(root: string, requests: ListEntry[]): Promise<Map<string, boolean>> {
    const key = `${root}|${this.envs.current(root) ?? ""}|${requests.map((r) => r.id).join(",")}`;
    let p = this.readiness.get(key);
    if (!p) {
      p = Promise.all(
        requests.map(async (r) => {
          const res = await this.apic.json<Description>(["describe", targetOf(r), ...this.envs.args(root)], { project: root }).catch(() => undefined);
          return [r.id, res?.value?.ready] as const;
        }),
      ).then((pairs) => {
        const out = new Map<string, boolean>();
        for (const [id, ready] of pairs) {
          if (ready !== undefined) {
            out.set(id, ready);
          }
        }
        return out;
      });
      this.readiness.set(key, p);
    }
    return p;
  }
}
