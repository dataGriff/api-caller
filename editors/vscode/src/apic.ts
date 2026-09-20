// Everything that talks to the apic binary goes through here. The
// extension never re-implements parsing or auth: it runs `apic … --json`
// and reads the result, so what the editor shows is what the CLI, CI and
// agents get.
import * as vscode from "vscode";
import { execFile } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";

/** The oldest apic this extension knows how to drive. */
export const MIN_VERSION = "0.1.2";

/** Where a missing or too-old apic is sent. */
export const INSTALL_URL = "https://datagriff.github.io/api-caller/getting-started/#1-install";

/** What `apic version --json` prints. */
export interface VersionInfo {
  version: string;
  commit: string;
  date?: string;
  go: string;
  os: string;
  arch: string;
}

/** The outcome of one apic invocation. Exit codes keep apic's meaning: 0 ok, 1 assertion failed, 2 usage, 3 network. */
export interface Result {
  code: number;
  stdout: string;
  stderr: string;
  /** The run was cancelled through its AbortSignal before it finished. */
  aborted?: boolean;
}

export interface RunOptions {
  /** Directory to run in. */
  cwd?: string;
  /** Project root, passed as -C. */
  project?: string;
  /** Cancels the process. */
  signal?: AbortSignal;
  /** Extra environment variables for the process. */
  env?: Record<string, string>;
}

/** Thrown when apic cannot be found at all. */
export class NotInstalledError extends Error {
  constructor(readonly searched: string) {
    super(`apic was not found (${searched}). Install it or set apic.path.`);
    this.name = "NotInstalledError";
  }
}

/**
 * Resolves the binary: the `apic.path` setting when set, else the first
 * `apic` (or `apic.exe`) on PATH. Cached per activation; `reset` forgets.
 */
export class Apic {
  private resolved: string | undefined;
  private info: VersionInfo | undefined;

  constructor(private readonly output: vscode.OutputChannel) {}

  reset(): void {
    this.resolved = undefined;
    this.info = undefined;
  }

  /** The binary's path, resolving it on first use. */
  async binary(): Promise<string> {
    if (this.resolved) {
      return this.resolved;
    }
    const configured = vscode.workspace.getConfiguration("apic").get<string>("path", "").trim();
    if (configured) {
      if (!fs.existsSync(configured)) {
        throw new NotInstalledError(`apic.path is ${configured}, which does not exist`);
      }
      this.resolved = configured;
      return configured;
    }
    const found = findOnPath("apic");
    if (!found) {
      throw new NotInstalledError("not on PATH");
    }
    this.resolved = found;
    return found;
  }

  /** `apic version --json`, cached. */
  async version(): Promise<VersionInfo> {
    if (this.info) {
      return this.info;
    }
    const res = await this.run(["version", "--json"]);
    if (res.code !== 0) {
      throw new Error(`apic version failed: ${res.stderr.trim() || `exit ${res.code}`}`);
    }
    this.info = JSON.parse(res.stdout) as VersionInfo;
    return this.info;
  }

  /**
   * Runs apic with args. `cwd` is the directory to run in; `-C` is added
   * for the project when given. Output is logged to the channel; nothing
   * is thrown for a non-zero exit, since 1, 2 and 3 all carry meaning.
   */
  async run(args: string[], opts: RunOptions = {}): Promise<Result> {
    const bin = await this.binary();
    const full = opts.project ? ["-C", opts.project, ...args] : args;
    this.output.appendLine(`$ apic ${full.join(" ")}`);
    return new Promise((resolve) => {
      execFile(
        bin,
        full,
        {
          cwd: opts.cwd,
          env: { ...process.env, NO_COLOR: "1", ...opts.env },
          maxBuffer: 64 * 1024 * 1024,
          signal: opts.signal,
        },
        (err, stdout, stderr) => {
          const aborted = err?.name === "AbortError" || (err as NodeJS.ErrnoException | null)?.code === "ABORT_ERR";
          const code = err && typeof (err as NodeJS.ErrnoException).code === "number" ? ((err as NodeJS.ErrnoException).code as unknown as number) : err ? 1 : 0;
          if (stderr) {
            this.output.appendLine(stderr.trimEnd());
          }
          resolve({ code, stdout, stderr, aborted });
        },
      );
    });
  }

  /** Runs apic and parses its stdout as JSON. */
  async json<T>(args: string[], opts: RunOptions = {}): Promise<{ code: number; value: T | undefined; stderr: string }> {
    const res = await this.run([...args, "--json"], opts);
    let value: T | undefined;
    if (res.stdout.trim()) {
      try {
        value = JSON.parse(res.stdout) as T;
      } catch {
        value = undefined;
      }
    }
    return { code: res.code, value, stderr: res.stderr };
  }

  /** Runs apic and parses its stdout as NDJSON: one object per non-empty line, as `run --json` prints. */
  async ndjson<T>(args: string[], opts: RunOptions = {}): Promise<{ code: number; values: T[]; stderr: string; aborted: boolean }> {
    const res = await this.run([...args, "--json"], opts);
    const values: T[] = [];
    for (const line of res.stdout.split(/\r?\n/)) {
      if (!line.trim()) {
        continue;
      }
      try {
        values.push(JSON.parse(line) as T);
      } catch {
        this.output.appendLine(`unparsable line: ${line}`);
      }
    }
    return { code: res.code, values, stderr: res.stderr, aborted: Boolean(res.aborted) };
  }
}

/** Finds an executable on PATH the way a shell would, on every OS. */
export function findOnPath(name: string): string | undefined {
  const dirs = (process.env.PATH ?? "").split(path.delimiter).filter(Boolean);
  const exts = process.platform === "win32" ? (process.env.PATHEXT ?? ".EXE;.CMD;.BAT").split(";") : [""];
  for (const dir of dirs) {
    for (const ext of exts) {
      const candidate = path.join(dir, name + ext.toLowerCase());
      try {
        fs.accessSync(candidate, fs.constants.X_OK);
        return candidate;
      } catch {
        // keep looking
      }
    }
  }
  return undefined;
}

/**
 * Compares two versions like "0.1.2" or "v0.2.0"; "dev" and other
 * non-numeric versions count as newest, since they are local builds.
 */
export function compareVersions(a: string, b: string): number {
  const parse = (v: string): number[] | undefined => {
    const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec(v.trim());
    return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : undefined;
  };
  const pa = parse(a);
  const pb = parse(b);
  if (!pa && !pb) {
    return 0;
  }
  if (!pa) {
    return 1;
  }
  if (!pb) {
    return -1;
  }
  for (let i = 0; i < 3; i++) {
    if (pa[i] !== pb[i]) {
      return pa[i] - pb[i];
    }
  }
  return 0;
}
