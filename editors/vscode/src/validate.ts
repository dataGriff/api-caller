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

/** Maps one diagnostic to editor coordinates. */
export function toProblem(d: ValidateDiagnostic): Problem {
  const line = Math.max(0, (d.line ?? 0) - 1);
  const hasSpan = typeof d.column === "number" && d.column > 0;
  if (!hasSpan) {
    return { line, startColumn: 0, endLine: line, endColumn: LINE_END, wholeLine: true, severity: d.severity, code: d.code, message: d.message };
  }
  const endLine = d.end_line && d.end_line > 0 ? d.end_line - 1 : line;
  const endColumn = d.end_column && d.end_column > d.column! ? d.end_column - 1 : d.column! - 1 + 1;
  return {
    line,
    startColumn: d.column! - 1,
    endLine,
    endColumn: Math.max(endColumn, d.column!),
    wholeLine: false,
    severity: d.severity,
    code: d.code,
    message: d.message,
  };
}

/** Groups the findings by the path apic reported, relative to the project root. */
export function problemsByPath(out: ValidateOutput): Map<string, Problem[]> {
  const byPath = new Map<string, Problem[]>();
  for (const d of out.diagnostics) {
    const list = byPath.get(d.path) ?? [];
    list.push(toProblem(d));
    byPath.set(d.path, list);
  }
  return byPath;
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
