// The Test Explorer: every `.feature` file of every project in the
// workspace (those under `test.paths` of its apic.yaml, `features` by
// default), parsed lightly into feature, scenario and example-row items,
// and run through `apic test --json` with the picked environment and an
// optional tag expression. The cucumber JSON apic prints becomes the
// result of each item, the failing step's message at its line.
import * as vscode from "vscode";
import * as fs from "node:fs";
import * as path from "node:path";
import type { Apic } from "./apic";
import type { Environments } from "./environment";
import { parseFeature, rowLabel, testPathsFromYaml, type Feature } from "./featureParser";
import { projectRoot } from "./project";
import { outcomeLines, parseCucumber, sameFile, scenarioOutcomes, type ScenarioOutcome } from "./testResults";

interface ItemData {
  kind: "root" | "file" | "scenario" | "row";
  root: string;
  /** The feature file, for file, scenario and row items. */
  file?: string;
  /** The scenario's line, or the example row's. */
  line?: number;
}

/** A parsed feature and where it lives. */
export interface ParsedFeature {
  uri: string;
  root: string;
  feature: Feature;
}

/** What one `apic test` run of some files produced. */
export interface RunOutcome {
  outcomes: ScenarioOutcome[];
  code: number;
  stderr: string;
}

const TAGS_KEY = "apic.test.tags";

export class TestExplorer implements vscode.Disposable {
  readonly controller: vscode.TestController;
  private readonly parsed = new Map<string, ParsedFeature>();
  private readonly data = new WeakMap<vscode.TestItem, ItemData>();
  private readonly changed = new vscode.EventEmitter<void>();
  /** Fires when the set of parsed features changed. */
  readonly onDidChangeFeatures = this.changed.event;
  private readonly disposables: vscode.Disposable[] = [];
  private discovering: Promise<void> | undefined;

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly apic: Apic,
    private readonly envs: Environments,
    private readonly output: vscode.OutputChannel,
  ) {
    this.controller = vscode.tests.createTestController("apic", "apic features");
    this.controller.refreshHandler = () => this.discover();
    this.controller.resolveHandler = async (item) => {
      if (!item) {
        await this.discover();
      }
    };
    const run = this.controller.createRunProfile("Run", vscode.TestRunProfileKind.Run, (req, token) => this.run(req, token, this.tagExpression()), true);
    run.configureHandler = () => void this.configureTags();
    this.controller.createRunProfile("Run with tags…", vscode.TestRunProfileKind.Run, async (req, token) => {
      const tags = await vscode.window.showInputBox({ prompt: "Tag expression for apic test --tags", placeHolder: "@smoke && ~@slow", value: this.tagExpression() });
      if (tags === undefined) {
        return;
      }
      await this.run(req, token, tags);
    });
    const features = vscode.workspace.createFileSystemWatcher("**/*.feature");
    const configs = vscode.workspace.createFileSystemWatcher("**/apic.yaml");
    this.disposables.push(
      this.controller,
      this.changed,
      run,
      features,
      configs,
      features.onDidChange((uri) => this.reparse(uri)),
      features.onDidCreate(() => this.discover()),
      features.onDidDelete(() => this.discover()),
      configs.onDidChange(() => this.discover()),
      configs.onDidCreate(() => this.discover()),
      configs.onDidDelete(() => this.discover()),
    );
  }

  dispose(): void {
    vscode.Disposable.from(...this.disposables).dispose();
  }

  /** The tag expression the Run profile passes as --tags, or "" for none. */
  tagExpression(): string {
    return this.context.workspaceState.get<string>(TAGS_KEY, "");
  }

  private async configureTags(): Promise<void> {
    const tags = await vscode.window.showInputBox({ prompt: "Tag expression every Test Explorer run passes as apic test --tags (empty for none)", placeHolder: "@smoke && ~@slow", value: this.tagExpression() });
    if (tags !== undefined) {
      await this.context.workspaceState.update(TAGS_KEY, tags.trim());
    }
  }

  /** The parsed features of a project, in path order. */
  featuresIn(root: string): ParsedFeature[] {
    return [...this.parsed.values()].filter((f) => f.root === root).sort((a, b) => a.uri.localeCompare(b.uri));
  }

  /** Finds every feature file of every project and rebuilds the tree. One discovery runs at a time. */
  discover(): Promise<void> {
    if (!this.discovering) {
      this.discovering = this.doDiscover().finally(() => {
        this.discovering = undefined;
      });
    }
    return this.discovering;
  }

  private async doDiscover(): Promise<void> {
    const files = await vscode.workspace.findFiles("**/*.feature", "**/node_modules/**", 2000);
    const byRoot = new Map<string, vscode.Uri[]>();
    for (const f of files) {
      const root = projectRoot(f);
      if (!root) {
        continue;
      }
      if (!underTestPaths(root, f.fsPath)) {
        continue;
      }
      const list = byRoot.get(root) ?? [];
      list.push(f);
      byRoot.set(root, list);
    }
    this.parsed.clear();
    this.controller.items.replace([]);
    for (const [root, uris] of [...byRoot.entries()].sort(([a], [b]) => a.localeCompare(b))) {
      const rootItem = this.controller.createTestItem(root, path.basename(root), vscode.Uri.file(root));
      rootItem.description = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders.length > 1 ? root : undefined;
      this.data.set(rootItem, { kind: "root", root });
      for (const uri of uris.sort((a, b) => a.fsPath.localeCompare(b.fsPath))) {
        const item = this.fileItem(root, uri);
        if (item) {
          rootItem.children.add(item);
        }
      }
      this.controller.items.add(rootItem);
    }
    this.changed.fire();
  }

  private fileItem(root: string, uri: vscode.Uri): vscode.TestItem | undefined {
    let text: string;
    try {
      text = fs.readFileSync(uri.fsPath, "utf8");
    } catch {
      return undefined;
    }
    const feature = parseFeature(text);
    if (!feature) {
      return undefined;
    }
    this.parsed.set(uri.fsPath, { uri: uri.fsPath, root, feature });
    const rel = path.relative(root, uri.fsPath).split(path.sep).join("/");
    const item = this.controller.createTestItem(uri.fsPath, feature.name || rel, uri);
    item.description = rel;
    item.range = new vscode.Range(feature.line - 1, 0, feature.line - 1, 0);
    item.tags = feature.tags.map((t) => new vscode.TestTag(t));
    this.data.set(item, { kind: "file", root, file: uri.fsPath });
    for (const sc of feature.scenarios) {
      const scItem = this.controller.createTestItem(`${uri.fsPath}:${sc.line}`, sc.name || `${sc.keyword} at line ${sc.line}`, uri);
      scItem.range = new vscode.Range(sc.line - 1, 0, sc.line - 1, 0);
      scItem.description = sc.tags.filter((t) => !feature.tags.includes(t)).join(" ") || undefined;
      scItem.tags = sc.tags.map((t) => new vscode.TestTag(t));
      this.data.set(scItem, { kind: "scenario", root, file: uri.fsPath, line: sc.line });
      for (const ex of sc.examples) {
        for (const row of ex.rows) {
          const rowItem = this.controller.createTestItem(`${uri.fsPath}:${row.line}`, rowLabel(row, ex.header), uri);
          rowItem.range = new vscode.Range(row.line - 1, 0, row.line - 1, 0);
          rowItem.description = ex.name || undefined;
          this.data.set(rowItem, { kind: "row", root, file: uri.fsPath, line: row.line });
          scItem.children.add(rowItem);
        }
      }
      item.children.add(scItem);
    }
    return item;
  }

  /** A feature file changed on disk: rebuild its item in place. */
  private reparse(uri: vscode.Uri): void {
    const existing = this.parsed.get(uri.fsPath);
    if (!existing) {
      void this.discover(); // new to us, or outside test.paths until now
      return;
    }
    const rootItem = this.controller.items.get(existing.root);
    const item = this.fileItem(existing.root, uri);
    if (rootItem && item) {
      rootItem.children.add(item); // same id: replaces
    }
    this.changed.fire();
  }

  /** The leaf items (scenarios, or the rows of an outline) under an item. */
  private leaves(item: vscode.TestItem, into: vscode.TestItem[] = []): vscode.TestItem[] {
    if (item.children.size === 0) {
      into.push(item);
    } else {
      item.children.forEach((c) => this.leaves(c, into));
    }
    return into;
  }

  private async run(request: vscode.TestRunRequest, token: vscode.CancellationToken, tags: string): Promise<void> {
    const roots: vscode.TestItem[] = [];
    if (request.include) {
      roots.push(...request.include);
    } else {
      this.controller.items.forEach((i) => roots.push(i));
    }
    const excluded = new Set(request.exclude ?? []);
    const byRoot = new Map<string, { files: Set<string>; leaves: vscode.TestItem[] }>();
    for (const item of roots) {
      const d = this.data.get(item);
      if (!d) {
        continue;
      }
      const group = byRoot.get(d.root) ?? { files: new Set<string>(), leaves: [] };
      for (const leaf of this.leaves(item)) {
        const ld = this.data.get(leaf);
        if (!ld?.file || excluded.has(leaf) || excluded.has(item)) {
          continue;
        }
        group.files.add(ld.file);
        group.leaves.push(leaf);
      }
      byRoot.set(d.root, group);
    }
    const run = this.controller.createTestRun(request, tags ? `apic test --tags ${tags}` : "apic test");
    const controller = new AbortController();
    token.onCancellationRequested(() => controller.abort());
    try {
      for (const [root, group] of byRoot) {
        if (token.isCancellationRequested) {
          break;
        }
        group.leaves.forEach((l) => run.enqueued(l));
        const files = [...group.files].map((f) => path.relative(root, f).split(path.sep).join("/")).sort();
        group.leaves.forEach((l) => run.started(l));
        const res = await this.runFiles(root, files, { tags, signal: controller.signal });
        if (controller.signal.aborted) {
          break;
        }
        this.report(run, root, group.leaves, res);
      }
    } finally {
      run.end();
    }
  }

  /**
   * Runs `apic test` for feature files of a project (paths relative to
   * its root) and maps the cucumber JSON to outcomes. Exit codes 2 and 3
   * come back with an empty outcome list and apic's stderr.
   */
  async runFiles(root: string, files: string[], opts: { tags?: string; signal?: AbortSignal } = {}): Promise<RunOutcome> {
    const args = ["test", ...files, ...this.envs.args(root), ...(opts.tags ? ["--tags", opts.tags] : [])];
    const showOutput = vscode.workspace.getConfiguration("apic", vscode.Uri.file(root)).get<boolean>("test.showOutput", false);
    const pretty = showOutput ? this.apic.run([...args, "--format", "pretty"], { project: root, signal: opts.signal }) : undefined;
    const res = await this.apic.run([...args, "--json"], { project: root, signal: opts.signal });
    if (pretty) {
      const p = await pretty;
      this.output.appendLine(p.stdout.trimEnd());
      this.output.show(true);
    }
    const features = parseCucumber(res.stdout);
    return { outcomes: features ? scenarioOutcomes(features) : [], code: res.code, stderr: res.stderr };
  }

  private report(run: vscode.TestRun, root: string, leaves: vscode.TestItem[], res: RunOutcome): void {
    if (res.outcomes.length === 0 && (res.code === 2 || res.code === 3)) {
      const text = res.stderr.trim() || `apic test exited with ${res.code}`;
      run.appendOutput(text.replace(/\r?\n/g, "\r\n") + "\r\n");
      leaves.forEach((l) => run.errored(l, new vscode.TestMessage(text)));
      void vscode.window.showErrorMessage(`apic test: ${text.split("\n")[0]}`, "Show output").then((choice) => {
        if (choice) {
          this.output.show(true);
        }
      });
      return;
    }
    for (const leaf of leaves) {
      const d = this.data.get(leaf)!;
      const outcome = res.outcomes.find((o) => o.line === d.line && sameFile(o.uri, root, d.file!));
      if (!outcome) {
        run.skipped(leaf); // filtered out by the tag expression
        continue;
      }
      run.appendOutput(outcomeLines(outcome).join("\r\n") + "\r\n", undefined, leaf);
      switch (outcome.status) {
        case "passed":
          run.passed(leaf, outcome.durationMs);
          break;
        case "skipped":
          run.skipped(leaf);
          break;
        case "failed": {
          const message = new vscode.TestMessage(outcome.failure?.message ?? "failed");
          if (outcome.failure) {
            message.location = new vscode.Location(leaf.uri!, new vscode.Position(outcome.failure.line - 1, 0));
          }
          run.failed(leaf, message, outcome.durationMs);
          break;
        }
      }
    }
  }
}

/** Whether a feature file is one `apic test` would run by default: under `test.paths` of the project's apic.yaml, or `features/` without one. */
export function underTestPaths(root: string, fsPath: string): boolean {
  let paths: string[] | undefined;
  try {
    paths = testPathsFromYaml(fs.readFileSync(path.join(root, "apic.yaml"), "utf8"));
  } catch {
    paths = undefined;
  }
  const rel = path.relative(root, fsPath).split(path.sep).join("/");
  return (paths ?? ["features"]).some((p) => {
    const clean = p.replace(/^\.\//, "").replace(/\/+$/, "");
    return rel === clean || rel.startsWith(`${clean}/`);
  });
}
