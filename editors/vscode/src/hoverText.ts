// The text shown when hovering a `{{placeholder}}`: its value (masked when
// secret), where it came from, and for a missing one, the request that
// captures it. Pure: the provider in hover.ts supplies what `apic
// describe` and `apic env` said.
import { BUILTINS } from "./complete";
import type { Description, VarInfo } from "./types";

const placeholderRe = /\{\{\s*([^{}]*?)\s*\}\}/g;

/** The placeholder around `column` on a line: its inner text and span, or undefined. */
export function placeholderAt(line: string, column: number): { name: string; start: number; end: number } | undefined {
  for (const m of line.matchAll(placeholderRe)) {
    const start = m.index;
    const end = start + m[0].length;
    if (column >= start && column <= end) {
      return { name: m[1], start, end };
    }
  }
  return undefined;
}

export interface HoverSources {
  /** `apic describe` of the request the placeholder is in, when it could be run. */
  description?: Description;
  /** `apic env` variables, the fallback outside a request. */
  env?: VarInfo[];
}

/** Markdown for a placeholder, or undefined when there is nothing to say. */
export function hoverText(inner: string, src: HoverSources): string | undefined {
  const name = inner.split(/\s+/)[0];
  if (!name) {
    return undefined;
  }
  if (name.startsWith("$")) {
    const builtin = BUILTINS.find((b) => b.name === name || (b.name === "$env" && name.startsWith("$env.")));
    return builtin ? `**${name}** · built-in\n\n${builtin.doc}` : `**${name}** · unknown built-in`;
  }
  const ref = /^([\w-]+)\.response\.(body|headers)(.*)$/.exec(name);
  if (ref) {
    const what = ref[2] === "body" ? "body" : "header";
    return `**${name}** · response reference\n\nThe ${what} of \`${ref[1]}\` from earlier in the same run.`;
  }
  const v = src.description?.variables?.find((x) => x.name === name) ?? src.env?.find((x) => x.name === name);
  if (!v) {
    return src.description ? `**${name}** · not set\n\nNo source defines it. Add it to an env file, \`.env\` or \`--var\`.` : undefined;
  }
  if (v.missing) {
    const lines = [`**${name}** · not set`];
    if (v.captured_by) {
      lines.push(v.ref_runs ? `Captured by \`${v.captured_by}\`, which \`# @ref\` runs first.` : `Captured by \`${v.captured_by}\`: run it first, or add \`# @ref ${v.captured_by}\`.`);
    } else {
      lines.push("No source defines it. Add it to an env file, `.env` or `--var`.");
    }
    return lines.join("\n\n");
  }
  const value = v.secret ? "***" : (v.value ?? "");
  return `**${name}** = \`${value}\`\n\nfrom ${v.source}${v.secret ? " (secret, masked)" : ""}`;
}
