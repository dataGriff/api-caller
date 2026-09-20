// The "apic: Response" webview: one panel beside the editor, reused for
// every run and describe. It shows the HTML render.ts produces under a
// strict content security policy, and answers the page's requests to
// save a body or open an oversized one in an editor.
import * as vscode from "vscode";
import { escapeHtml, rawBody, renderDescription, renderRun, type RenderOptions } from "./render";
import type { Description, RunResult } from "./types";

type Message = { type: "save"; index: number } | { type: "open-raw"; index: number };

export class ResponsePanel implements vscode.Disposable {
  private panel: vscode.WebviewPanel | undefined;
  private results: RunResult[] = [];
  private lastHtml = "";

  constructor(private readonly context: vscode.ExtensionContext) {}

  dispose(): void {
    this.panel?.dispose();
  }

  /** The results of the last run shown, for tests and for "show last response". */
  last(): RunResult[] {
    return this.results;
  }

  /** Shows a run. */
  showRun(results: RunResult[], opts: RenderOptions = {}): void {
    this.results = results;
    this.render(renderRun(results, opts), results.length === 1 && results[0].request.name ? `apic: ${results[0].request.name}` : "apic: Response");
  }

  /** Shows what `apic describe` knows about a request. */
  showDescription(d: Description): void {
    this.results = [];
    this.render(renderDescription(d), `apic: ${d.id}`);
  }

  /** Re-opens the panel with whatever it showed last. */
  reveal(): void {
    if (this.lastHtml) {
      this.render(this.lastHtml, this.panel?.title ?? "apic: Response");
    } else {
      void vscode.window.showInformationMessage("No request has been run yet: use the Run lens above a request.");
    }
  }

  private render(body: string, title: string): void {
    const panel = this.ensure();
    panel.title = title;
    this.lastHtml = body;
    const nonce = [...Array(24)].map(() => Math.floor(Math.random() * 36).toString(36)).join("");
    const css = panel.webview.asWebviewUri(vscode.Uri.joinPath(this.context.extensionUri, "media", "response.css"));
    panel.webview.html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${panel.webview.cspSource}; script-src 'nonce-${nonce}';">
<link rel="stylesheet" href="${css}">
<title>${escapeHtml(title)}</title>
</head>
<body>
${body}
<script nonce="${nonce}">
(function () {
  const vscode = acquireVsCodeApi();
  document.body.addEventListener("click", (event) => {
    const button = event.target.closest("button[data-action]");
    if (!button) { return; }
    const index = Number(button.dataset.index);
    if (button.dataset.action === "toggle-raw") {
      const wrap = button.closest(".body-wrap");
      const pretty = wrap.querySelector("pre.pretty");
      const raw = wrap.querySelector("pre.raw");
      if (raw) {
        pretty.classList.toggle("hidden");
        raw.classList.toggle("hidden");
        button.textContent = raw.classList.contains("hidden") ? "Raw" : "Pretty";
      }
      return;
    }
    vscode.postMessage({ type: button.dataset.action, index });
  });
})();
</script>
</body>
</html>`;
    panel.reveal(undefined, true);
  }

  private ensure(): vscode.WebviewPanel {
    if (this.panel) {
      return this.panel;
    }
    const panel = vscode.window.createWebviewPanel("apic.response", "apic: Response", { viewColumn: vscode.ViewColumn.Beside, preserveFocus: true }, {
      enableScripts: true,
      retainContextWhenHidden: true,
      localResourceRoots: [vscode.Uri.joinPath(this.context.extensionUri, "media")],
    });
    panel.onDidDispose(() => {
      this.panel = undefined;
    });
    panel.webview.onDidReceiveMessage((m: Message) => void this.onMessage(m));
    this.panel = panel;
    return panel;
  }

  private async onMessage(m: Message): Promise<void> {
    const r = this.results[m.index];
    if (!r?.response) {
      return;
    }
    const body = rawBody(r.response.body);
    const json = typeof r.response.body !== "string";
    if (m.type === "open-raw") {
      const doc = await vscode.workspace.openTextDocument({ content: body, language: json ? "json" : "plaintext" });
      await vscode.window.showTextDocument(doc, { preview: true, viewColumn: vscode.ViewColumn.Active });
      return;
    }
    if (m.type === "save") {
      const name = (r.request.name ?? "response") + (json ? ".json" : ".txt");
      const folder = vscode.workspace.workspaceFolders?.[0]?.uri;
      const target = await vscode.window.showSaveDialog({ defaultUri: folder ? vscode.Uri.joinPath(folder, name) : undefined, saveLabel: "Save body" });
      if (target) {
        await vscode.workspace.fs.writeFile(target, Buffer.from(body, "utf8"));
      }
    }
  }
}
