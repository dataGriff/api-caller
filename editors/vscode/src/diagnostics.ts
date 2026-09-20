// Problems from `apic validate --json`, published for every file in a
// project whenever a request file, apic.yaml or an env file changes on
// disk. apic validates from disk, so the trigger is the save (or an
// external change), not the keystroke; an unsaved buffer keeps the
// diagnostics of its last save.
import * as vscode from "vscode";
import * as fs from "node:fs";
import * as path from "node:path";
import type { Apic } from "./apic";
import { projectRoot } from "./project";
import { parseUsageError, parseValidateOutput, problemsByPath, LINE_END } from "./validate";

/** Where a diagnostic's code links to. */
export const CODES_URL = "https://datagriff.github.io/api-caller/cli/#apic-validate";

/** Files whose change means the project should be validated again (and its request list refreshed). */
export function triggersValidation(uri: vscode.Uri): boolean {
  const base = path.basename(uri.fsPath);
  const ext = path.extname(base).toLowerCase();
  return ext === ".http" || ext === ".rest" || base === "apic.yaml" || base === "http-client.env.json" || base === "http-client.private.env.json" || base === ".env";
}

/** The glob for a watcher over the same files. */
export const WATCH_GLOB = "**/{*.http,*.rest,apic.yaml,http-client.env.json,http-client.private.env.json,.env}";

export class Diagnostics implements vscode.Disposable {
  readonly collection: vscode.DiagnosticCollection;
  private readonly status: vscode.StatusBarItem;
  private readonly reported = new Map<string, Set<string>>();
  private readonly inFlight = new Map<string, Promise<void>>();
  private readonly again = new Set<string>();
  private readonly pending = new Map<string, NodeJS.Timeout>();
  private readonly disposables: vscode.Disposable[] = [];

  constructor(private readonly apic: Apic) {
    this.collection = vscode.languages.createDiagnosticCollection("apic");
    this.status = vscode.window.createStatusBarItem("apic.problems", vscode.StatusBarAlignment.Left, 49);
    this.status.name = "apic problems";
    this.status.command = "workbench.actions.view.problems";
    this.disposables.push(this.collection, this.status);
  }

  dispose(): void {
    for (const t of this.pending.values()) {
      clearTimeout(t);
    }
    vscode.Disposable.from(...this.disposables).dispose();
  }

  /** Whether validation runs on its own (activation, saves, external changes) or only on request. */
  auto(): boolean {
    return vscode.workspace.getConfiguration("apic").get<boolean>("validate.auto", true);
  }

  /** A file changed on disk: validate its project soon, when validation is automatic. */
  changed(uri: vscode.Uri): void {
    if (uri.scheme !== "file" || !triggersValidation(uri) || !this.auto()) {
      return;
    }
    const root = projectRoot(uri);
    if (root) {
      this.schedule(root);
    }
  }

  /** Validates soon, coalescing a burst of saves into one run per project. */
  schedule(root: string): void {
    const delay = Math.max(0, vscode.workspace.getConfiguration("apic").get<number>("validate.debounceMs", 300));
    const existing = this.pending.get(root);
    if (existing) {
      clearTimeout(existing);
    }
    this.pending.set(
      root,
      setTimeout(() => {
        this.pending.delete(root);
        void this.validate(root);
      }, delay),
    );
  }

  /** Validates every project that has a request file in the workspace. `force` ignores the auto setting, for the command. */
  async validateWorkspace(force = false): Promise<void> {
    if (!force && !this.auto()) {
      return;
    }
    const files = await vscode.workspace.findFiles("**/*.{http,rest}", "**/node_modules/**", 200);
    const roots = new Set<string>();
    for (const f of files) {
      const root = projectRoot(f);
      if (root) {
        roots.add(root);
      }
    }
    await Promise.all([...roots].map((root) => this.validate(root)));
  }

  /**
   * Runs `apic validate --json` for one project and publishes what it
   * reports. A call while a run is in flight waits for it and then runs
   * again, since the disk may have changed after the first run read it.
   */
  validate(root: string): Promise<void> {
    const running = this.inFlight.get(root);
    if (running) {
      this.again.add(root);
      return running;
    }
    const p = this.doValidate(root)
      .finally(() => this.inFlight.delete(root))
      .then(() => {
        if (this.again.delete(root)) {
          return this.validate(root);
        }
        return undefined;
      });
    this.inFlight.set(root, p);
    return p;
  }

  private async doValidate(root: string): Promise<void> {
    let res: { code: number; stdout: string; stderr: string };
    try {
      res = await this.apic.run(["validate", "--json"], { project: root });
    } catch {
      return; // no binary: the activation prompt already said so
    }
    const out = parseValidateOutput(res.stdout);
    const seen = new Set<string>();
    const publish = (rel: string, diags: vscode.Diagnostic[]) => {
      const uri = vscode.Uri.file(path.join(root, rel));
      seen.add(uri.toString());
      this.collection.set(uri, diags);
    };
    if (out) {
      for (const [rel, problems] of problemsByPath(out, (file) => lineTexts(path.join(root, file)))) {
        publish(
          rel,
          problems.map((p) => {
            const range = new vscode.Range(p.line, p.startColumn, p.endLine, p.wholeLine ? LINE_END : p.endColumn);
            const d = new vscode.Diagnostic(range, p.message, p.severity === "error" ? vscode.DiagnosticSeverity.Error : vscode.DiagnosticSeverity.Warning);
            d.source = "apic";
            if (p.code) {
              d.code = { value: p.code, target: vscode.Uri.parse(CODES_URL) };
            }
            return d;
          }),
        );
      }
    } else {
      // No report at all: the project could not be loaded (a broken
      // apic.yaml, an env file that is not JSON). That is the one problem
      // worth showing, on the file the error names.
      const usage = parseUsageError(res.stderr);
      if (!usage) {
        return; // an older apic, or a crash: leave what is shown alone
      }
      const d = new vscode.Diagnostic(new vscode.Range(usage.line, 0, usage.line, LINE_END), usage.message, vscode.DiagnosticSeverity.Error);
      d.source = "apic";
      publish(usage.path, [d]);
    }
    // Files this project reported last time and not now are clean.
    for (const old of this.reported.get(root) ?? []) {
      if (!seen.has(old)) {
        this.collection.delete(vscode.Uri.parse(old));
      }
    }
    this.reported.set(root, seen);
    this.refreshStatus();
  }

  /** The number of problems shown, across projects. */
  count(): { errors: number; warnings: number } {
    let errors = 0;
    let warnings = 0;
    this.collection.forEach((_uri, diags) => {
      for (const d of diags) {
        if (d.severity === vscode.DiagnosticSeverity.Error) {
          errors++;
        } else {
          warnings++;
        }
      }
    });
    return { errors, warnings };
  }

  private refreshStatus(): void {
    const { errors, warnings } = this.count();
    const total = errors + warnings;
    if (total === 0) {
      this.status.text = "$(check) apic";
      this.status.tooltip = "apic validate: no problems";
    } else {
      this.status.text = `$(warning) apic: ${total} problem${total === 1 ? "" : "s"}`;
      this.status.tooltip = `apic validate: ${errors} error${errors === 1 ? "" : "s"}, ${warnings} warning${warnings === 1 ? "" : "s"} (click to open Problems)`;
    }
    this.status.show();
  }
}

/** The lines of a file on disk, for converting apic's byte columns; empty when it cannot be read. */
function lineTexts(file: string): string[] {
  try {
    return fs.readFileSync(file, "utf8").split(/\r?\n/);
  } catch {
    return [];
  }
}
