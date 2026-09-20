// Where the requests in a file are, for CodeLens and "run under cursor".
// The truth comes from `apic list --json`; a light scan of the text is
// the fallback for a file the project cannot parse, so the lenses still
// appear and point at the diagnostic. Pure: unit-tested under node.
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

/** The positions `apic list` reports for one file. */
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
  return positions;
}

const separator = /^###/;
const comment = /^\s*(#|\/\/)/;
const fileVar = /^\s*@[A-Za-z_][\w.-]*\s*=/;
const nameDirective = /^\s*(?:#|\/\/)\s*@name\s+(\S+)/;
const requestLine = /^\s*([A-Z]+)\s+\S/;

/**
 * Finds request lines without the parser: after each `###` (or at the
 * top), the first line that is not blank, a comment, or a file variable
 * is the request line. Good enough to hang a lens on while the file is
 * broken; the name comes from `# @name` in the same block.
 */
export function scanRequests(text: string, file: string): RequestPosition[] {
  const rel = normalizeRelative(file);
  const positions: RequestPosition[] = [];
  const lines = text.split(/\r?\n/);
  let name: string | undefined;
  let want = true; // looking for the request line of the current block
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (separator.test(line)) {
      name = undefined;
      want = true;
      continue;
    }
    if (!want) {
      continue;
    }
    if (line.trim() === "" || fileVar.test(line)) {
      continue;
    }
    if (comment.test(line)) {
      const m = nameDirective.exec(line);
      if (m) {
        name = m[1];
      }
      continue;
    }
    const m = requestLine.exec(line);
    const method = m ? m[1] : "GET";
    const index = positions.length + 1;
    positions.push({ target: name ? `${rel}#${name}` : `${rel}#${index}`, name, line: i + 1, method });
    want = false;
  }
  return positions;
}

/** The request whose block contains a 1-based line, if any. */
export function requestAt(positions: RequestPosition[], line: number, text: string): RequestPosition | undefined {
  // A request's block runs from the separator (or directives) above it to
  // the line before the next separator; walk the text to find the bounds.
  const lines = text.split(/\r?\n/);
  let best: RequestPosition | undefined;
  for (const p of positions) {
    // Block start: the nearest `###` above the request line, else the top.
    let start = p.line - 1;
    while (start > 0 && !separator.test(lines[start - 1] ?? "")) {
      start--;
    }
    let end = p.line;
    while (end < lines.length && !separator.test(lines[end] ?? "")) {
      end++;
    }
    if (line >= start + 1 && line <= end) {
      best = p;
    }
  }
  return best;
}
