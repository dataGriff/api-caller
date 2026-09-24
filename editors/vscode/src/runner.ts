// Runs requests through the apic binary, one at a time like the terminal
// UI, with a cancellable progress notification, and hands the results to
// the panel and the decorations. Exit codes 2 and 3 surface the CLI's own
// error text, with a "Run login" action when the hint names a request and
// a link to the error's entry in the catalogue when apic gives a code.
import * as vscode from "vscode";
import type { Apic } from "./apic";
import type { Decorations } from "./decorations";
import type { Environments } from "./environment";
import type { ResponsePanel } from "./responsePanel";
import type { CurlOutput, Description, RunResult } from "./types";
import { parseStderrError } from "./validate";

const capturedBy = /captured by request "([^"]+)"/;

interface RunOutcome {
  results: RunResult[];
  /** Set when apic could not send something (exit 2 or 3): the stderr to show. */
  error?: { stderr: string; code: number };
}

export class Runner {
  /** Runs go through this one at a time. Nothing that waits on the user runs inside it. */
  private queue: Promise<unknown> = Promise.resolve();
  private readonly ran = new vscode.EventEmitter<string>();
  /** Fires with the project root after a run finished, whatever its outcome. */
  readonly onDidRun = this.ran.event;

  constructor(
    private readonly apic: Apic,
    private readonly envs: Environments,
    private readonly panel: ResponsePanel,
    private readonly decorations: Decorations,
    private readonly output: vscode.OutputChannel,
  ) {}

  private settings(root: string): { extraArgs: string[]; verbose: boolean } {
    const cfg = vscode.workspace.getConfiguration("apic", vscode.Uri.file(root));
    return { extraArgs: cfg.get<string[]>("run.extraArgs", []), verbose: cfg.get<boolean>("run.verbose", false) };
  }

  /** Runs the targets (request ids or files) as `apic run`; returns the results, or undefined when nothing could be sent. */
  async run(root: string, targets: string[], title?: string): Promise<RunResult[] | undefined> {
    const job = this.queue.then(() => this.doRun(root, targets, title));
    this.queue = job.catch(() => undefined);
    const outcome = await job;
    this.ran.fire(root);
    if (outcome.error) {
      // Outside the queue: the notification waits for the user, and the
      // action it offers starts a run of its own.
      await this.reportError(root, outcome.error.stderr, outcome.error.code);
    }
    return outcome.results.length > 0 ? outcome.results : undefined;
  }

  private async doRun(root: string, targets: string[], title: string | undefined): Promise<RunOutcome> {
    const { extraArgs, verbose } = this.settings(root);
    const args = ["run", ...targets, ...this.envs.args(root), ...(verbose ? ["-v"] : []), ...extraArgs];
    const redact = extraArgs.includes("--redact");
    const res = await vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title: `apic run ${targets.join(" ")}`, cancellable: true },
      (_progress, token) => {
        const controller = new AbortController();
        token.onCancellationRequested(() => controller.abort());
        return this.apic.ndjson<RunResult>(args, { project: root, signal: controller.signal });
      },
    );
    if (res.aborted) {
      void vscode.window.setStatusBarMessage("apic: run cancelled", 3000);
      return { results: [] };
    }
    if (res.values.length > 0) {
      this.panel.showRun(res.values, { redact, title: title ?? (targets.length > 1 || targets[0]?.endsWith(".http") ? targets.join(" ") : undefined) });
      this.decorations.show(root, res.values);
    }
    const error = res.code === 2 || res.code === 3 ? { stderr: res.stderr, code: res.code } : undefined;
    return { results: res.values, error };
  }

  private async reportError(root: string, stderr: string, code: number): Promise<void> {
    const err = parseStderrError(stderr);
    const body = err?.message ?? stderr;
    const text = body.trim().split("\n").slice(0, 4).join(" ").replace(/\s+/g, " ") || `apic exited with ${code}`;
    const hinted = capturedBy.exec(body)?.[1];
    const explain = err?.code && err.url ? `What is ${err.code}?` : undefined;
    const actions = [...(hinted ? [`Run ${hinted}`] : []), ...(explain ? [explain] : []), "Show output"];
    const choice = await vscode.window.showErrorMessage(text, ...actions);
    if (choice === "Show output") {
      this.output.show(true);
    } else if (choice && choice === explain && err?.url) {
      await vscode.env.openExternal(vscode.Uri.parse(err.url));
    } else if (choice && hinted) {
      await this.run(root, [hinted]);
    }
  }

  /** Shows `apic describe` for a request in the panel. */
  async describe(root: string, target: string): Promise<Description | undefined> {
    const res = await this.apic.json<Description>(["describe", target, ...this.envs.args(root)], { project: root });
    if (!res.value) {
      await this.reportError(root, res.stderr, res.code);
      return undefined;
    }
    this.panel.showDescription(res.value);
    return res.value;
  }

  /** Copies the curl command for a request to the clipboard. */
  async copyCurl(root: string, target: string, redact: boolean): Promise<string | undefined> {
    const res = await this.apic.json<CurlOutput>(["curl", target, ...this.envs.args(root), ...(redact ? ["--redact"] : [])], { project: root });
    if (!res.value) {
      await this.reportError(root, res.stderr, res.code);
      return undefined;
    }
    await vscode.env.clipboard.writeText(res.value.command);
    void vscode.window.setStatusBarMessage(`apic: curl for ${res.value.id} copied${redact ? " (redacted)" : ""}`, 3000);
    return res.value.command;
  }
}
