// Turns `apic validate --json` into editor problems. Pure: no vscode
// import, so it is unit-tested under plain node.
import type { ValidateDiagnostic, ValidateOutput } from "./types";

/** A finding positioned the way editors count: 0-based line and column, exclusive end. */
export interface Problem {
  line: number;
  startColumn: number;
  endLine: number;
  endColumn: number;
  /** True when apic gave no span: the range is the whole line (or the file's first line for line 0). */
  wholeLine: boolean;
  severity: "error" | "warning";
  code?: string;
  message: string;
}

/** A column past any real line, so a whole-line range clamps to the line's end. */
export const LINE_END = 1_000_000;

/** Parses the command's stdout; undefined when it is not the expected shape. */
export function parseValidateOutput(text: string): ValidateOutput | undefined {
  if (!text.trim()) {
    return undefined;
  }
  try {
    const value = JSON.parse(text) as Partial<ValidateOutput>;
    if (!value || !Array.isArray(value.diagnostics)) {
      return undefined;
    }
    return { ok: Boolean(value.ok), files: value.files ?? 0, requests: value.requests ?? 0, diagnostics: value.diagnostics };
  } catch {
    return undefined;
  }
}

/**
 * Converts a 1-based byte column, which is what apic reports, to a 0-based
 * UTF-16 column, which is what the editor counts. Without the line's text
 * the two are assumed equal, which holds for ASCII.
 */
export function byteToCharColumn(line: string | undefined, byteColumn: number): number {
  const target = byteColumn - 1;
  if (line === undefined || target <= 0) {
    return Math.max(0, target);
  }
  let bytes = 0;
  for (let i = 0; i < line.length; i++) {
    if (bytes >= target) {
      return i;
    }
    const code = line.codePointAt(i)!;
    bytes += code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4;
    if (code >= 0x10000) {
      i++; // a surrogate pair is one code point over two UTF-16 units
    }
  }
  return line.length + Math.max(0, target - bytes);
}

/** Maps one diagnostic to editor coordinates. `lines` is the file's text, for byte-to-character columns. */
export function toProblem(d: ValidateDiagnostic, lines: readonly string[] = []): Problem {
  const line = Math.max(0, (d.line ?? 0) - 1);
  const hasSpan = typeof d.column === "number" && d.column > 0;
  if (!hasSpan) {
    return { line, startColumn: 0, endLine: line, endColumn: LINE_END, wholeLine: true, severity: d.severity, code: d.code, message: d.message };
  }
  const endLine = d.end_line && d.end_line > 0 ? d.end_line - 1 : line;
  const startColumn = byteToCharColumn(lines[line], d.column!);
  const endByte = d.end_column && d.end_column > d.column! ? d.end_column : d.column! + 1;
  const endColumn = byteToCharColumn(lines[endLine], endByte);
  return {
    line,
    startColumn,
    endLine,
    endColumn: Math.max(endColumn, endLine === line ? startColumn + 1 : 0),
    wholeLine: false,
    severity: d.severity,
    code: d.code,
    message: d.message,
  };
}

/**
 * Groups the findings by the path apic reported, relative to the project
 * root. `textOf` supplies a file's lines so byte columns become character
 * columns; without it they are taken as equal.
 */
export function problemsByPath(out: ValidateOutput, textOf: (path: string) => readonly string[] = () => []): Map<string, Problem[]> {
  const byPath = new Map<string, Problem[]>();
  const texts = new Map<string, readonly string[]>();
  for (const d of out.diagnostics) {
    let lines = texts.get(d.path);
    if (!lines) {
      lines = textOf(d.path);
      texts.set(d.path, lines);
    }
    const list = byPath.get(d.path) ?? [];
    list.push(toProblem(d, lines));
    byPath.set(d.path, list);
  }
  return byPath;
}

/** An error apic reported on stderr, from the `--json` object when there is one. */
export interface ApicError {
  message: string;
  /** The catalogue code (E101…), from apic 0.2 on. */
  code?: string;
  title?: string;
  hint?: string;
  /** Where docs/errors.md explains the code. */
  url?: string;
}

/**
 * The error on apic's stderr: under `--json` one line holding
 * `{"error": {code, title, message, hint, exit, url}}`; from older
 * releases, or without `--json`, an `error: …` line.
 */
export function parseStderrError(stderr: string): ApicError | undefined {
  const lines = stderr.split(/\r?\n/).map((l) => l.trim());
  for (const line of lines) {
    if (!line.startsWith("{")) {
      continue;
    }
    try {
      const e = (JSON.parse(line) as { error?: ApicError }).error;
      if (e && typeof e.message === "string") {
        return { message: e.message, code: e.code, title: e.title, hint: e.hint, url: e.url };
      }
    } catch {
      // not the error object
    }
  }
  const at = lines.findIndex((l) => l.startsWith("error:"));
  if (at < 0) {
    return undefined;
  }
  // A message can run over several lines (one per missing variable).
  const rest = lines.slice(at + 1).filter((l) => l !== "");
  return { message: [lines[at].slice("error:".length).trim(), ...rest].join("\n") };
}

/**
 * The problem `apic validate` reports on stderr when it cannot load the
 * project at all (`apic.yaml: …`, `users.http:12: …`): the file, a 0-based
 * line (0 when none), and the message.
 */
export function parseUsageError(stderr: string): { path: string; line: number; message: string } | undefined {
  const err = parseStderrError(stderr);
  if (!err) {
    return undefined;
  }
  const text = err.message.split("\n")[0].trim();
  const m = /^([^\s:]+(?:\.[a-z]+)):(?:(\d+):)?\s*(.*)$/.exec(text);
  if (m) {
    return { path: m[1], line: m[2] ? Math.max(0, Number(m[2]) - 1) : 0, message: m[3] || text };
  }
  return { path: "apic.yaml", line: 0, message: text };
}

/** Levenshtein distance, for "did you mean" suggestions. */
export function levenshtein(a: string, b: string): number {
  const rows = a.length + 1;
  const cols = b.length + 1;
  const dist: number[] = new Array<number>(cols).fill(0).map((_, j) => j);
  for (let i = 1; i < rows; i++) {
    let prev = dist[0];
    dist[0] = i;
    for (let j = 1; j < cols; j++) {
      const tmp = dist[j];
      dist[j] = Math.min(dist[j] + 1, dist[j - 1] + 1, prev + (a[i - 1] === b[j - 1] ? 0 : 1));
      prev = tmp;
    }
  }
  return dist[cols - 1];
}

/** The known directive closest to `name`, when it is close enough to be a typo. */
export function nearestDirective(name: string, known: readonly string[]): string | undefined {
  let best: string | undefined;
  let bestDistance = Number.POSITIVE_INFINITY;
  const lower = name.toLowerCase();
  for (const k of known) {
    const d = levenshtein(lower, k.toLowerCase());
    if (d < bestDistance) {
      best = k;
      bestDistance = d;
    }
  }
  // Two edits for short names, up to three for long ones: `nmae` is a typo
  // of `name`, `frobnicate` is not a typo of anything.
  const limit = Math.min(3, Math.max(2, Math.floor(name.length / 2)));
  return best !== undefined && bestDistance <= limit ? best : undefined;
}

/**
 * The directives the extension's own grammar lists as known: the
 * `(@(?:a|b|c))` alternation of `syntaxes/apic-directives.injection.json`,
 * which a Go test keeps equal to the parser's list.
 */
export function directivesFromGrammar(grammarJson: string): string[] {
  try {
    const grammar = JSON.parse(grammarJson) as { patterns?: { name?: string; match?: string }[] };
    const pattern = grammar.patterns?.find((p) => p.name === "meta.directive.apic")?.match ?? "";
    const m = /\(@\(\?:([^)]*)\)\)/.exec(pattern);
    return m ? m[1].split("|") : [];
  } catch {
    return [];
  }
}
