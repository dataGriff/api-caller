// Finds the apic project a file belongs to: the directory apic is run in
// with -C, where apic.yaml, the env files and .apic/ live.
import * as vscode from "vscode";
import * as fs from "node:fs";
import * as path from "node:path";

/** Files whose presence marks a project root. */
const MARKERS = ["apic.yaml", "http-client.env.json", "http-client.private.env.json"];

/**
 * The project root for a document: the `apic.projectDir` setting when set
 * (relative to the workspace folder), else the nearest directory above the
 * file holding a marker, else the workspace folder, else the file's own
 * directory.
 */
export function projectRoot(uri?: vscode.Uri): string | undefined {
  const folder = uri ? vscode.workspace.getWorkspaceFolder(uri) : vscode.workspace.workspaceFolders?.[0];
  const configured = vscode.workspace.getConfiguration("apic", uri).get<string>("projectDir", "").trim();
  if (configured) {
    return path.isAbsolute(configured) || !folder ? configured : path.join(folder.uri.fsPath, configured);
  }
  if (uri?.scheme === "file") {
    let dir = path.dirname(uri.fsPath);
    const stop = folder?.uri.fsPath;
    for (;;) {
      if (MARKERS.some((m) => fs.existsSync(path.join(dir, m)))) {
        return dir;
      }
      if (dir === stop) {
        break;
      }
      const parent = path.dirname(dir);
      if (parent === dir) {
        break;
      }
      dir = parent;
    }
  }
  return folder?.uri.fsPath ?? (uri?.scheme === "file" ? path.dirname(uri.fsPath) : undefined);
}
