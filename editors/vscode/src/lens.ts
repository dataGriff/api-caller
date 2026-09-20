// Where the requests in a file are, for CodeLens and "run under cursor".
// The positions come from `apic list --json`, which reports every request
// even when a file has a parse error, so the extension never parses
// `.http` text itself. Pure: unit-tested under node.
import type { ListOutput } from "./types";

export interface RequestPosition {
  /** The run target: `file#name`, or `file#N` for an unnamed request. */
  target: string;
  name?: string;
  /** 1-based line of the request line. */
  line: number;
  method: string;
}

/** Normalises a path relative to the project root the way apic prints it. */
export function normalizeRelative(rel: string): string {
  return rel.split("\\").join("/").replace(/^\.\//, "");
}

/** The positions `apic list` reports for one file, in file order. */
export function positionsFromList(out: ListOutput, file: string): RequestPosition[] {
  const want = normalizeRelative(file);
  const positions: RequestPosition[] = [];
  let index = 0;
  for (const r of out.requests) {
    if (normalizeRelative(r.file) !== want) {
      continue;
    }
    index++;
    positions.push({
      target: r.name ? `${want}#${r.name}` : `${want}#${index}`,
      name: r.name,
      line: r.line,
      method: r.method,
    });
  }
  return positions.sort((a, b) => a.line - b.line);
}

const separator = /^###/;
const commentOrBlank = /^\s*(#|\/\/|$)/;

/**
 * The request the cursor is on, given the request lines apic reported: the
 * request line at or below the cursor when only comments (directives) and
 * blank lines lie between, else the nearest request line above. A `###`
 * in between means the cursor is in the previous request's body.
 */
export function requestAt(positions: RequestPosition[], line: number, text: string): RequestPosition | undefined {
  if (positions.length === 0) {
    return undefined;
  }
  const lines = text.split(/\r?\n/);
  const byLine = new Map(positions.map((p) => [p.line, p]));
  for (let i = line; i <= lines.length; i++) {
    const at = byLine.get(i);
    if (at) {
      return at;
    }
    const content = lines[i - 1] ?? "";
    if (separator.test(content)) {
      if (i === line) {
        continue; // the cursor is on a block's title: that block's request
      }
      break;
    }
    if (!commentOrBlank.test(content)) {
      break;
    }
  }
  let above: RequestPosition | undefined;
  for (const p of positions) {
    if (p.line < line && (!above || p.line > above.line)) {
      above = p;
    }
  }
  if (!above) {
    return undefined;
  }
  // A separator between the request line above and the cursor starts a new
  // block whose request line apic did not report (a request with an error
  // that stops at the separator): nothing to run there.
  for (let i = above.line + 1; i < line; i++) {
    if (separator.test(lines[i - 1] ?? "")) {
      return undefined;
    }
  }
  return above;
}
