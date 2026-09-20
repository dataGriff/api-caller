// The models behind the requests and session views, built from apic's
// JSON so they can be unit-tested under node without VS Code.
import type { ListEntry, ListOutput } from "./types";
import { normalizeRelative } from "./lens";

/** One file of a project with its requests in file order. */
export interface FileGroup {
  file: string;
  requests: ListEntry[];
}

/** Groups `apic list --json` by file, files sorted, requests in line order. */
export function groupByFile(out: ListOutput): FileGroup[] {
  const groups = new Map<string, ListEntry[]>();
  for (const r of out.requests) {
    const file = normalizeRelative(r.file);
    let g = groups.get(file);
    if (!g) {
      g = [];
      groups.set(file, g);
    }
    g.push(r);
  }
  return [...groups.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([file, requests]) => ({ file, requests: [...requests].sort((a, b) => a.line - b.line) }));
}

/** The run target for a request: its id, which apic prints as `name` or `file#N`. */
export function targetOf(r: ListEntry): string {
  return r.id;
}

/** The one-line label of a request in the tree: the method and the id. */
export function requestLabel(r: ListEntry): string {
  return `${r.method} ${r.id}`;
}

/** What the tree shows dimmed after the label. */
export function requestDetail(r: ListEntry): string {
  const bits: string[] = [];
  if (r.description) {
    bits.push(r.description);
  }
  if (r.asserts) {
    bits.push(`${r.asserts} assert${r.asserts === 1 ? "" : "s"}`);
  }
  if (r.captures?.length) {
    bits.push(`captures ${r.captures.join(", ")}`);
  }
  return bits.join(" · ");
}

/** A captured value as the session view shows it. */
export interface SessionItem {
  name: string;
  /** The value, or its description for a cached token, or `***` when masked. */
  shown: string;
  kind: "value" | "token" | "cookie";
}

/** Session keys the CLI prints as a token description rather than a value. */
export function isTokenKey(name: string): boolean {
  return name.startsWith("$oauth2:") || name.startsWith("$exec:");
}

/** A stored cookie as `apic session cookies --json` prints it. */
export interface CookieInfo {
  name: string;
  domain: string;
  path: string;
  expires?: string;
  secure?: boolean;
  http_only?: boolean;
}

/**
 * Builds the session view's items: captured values first (sorted, tokens
 * as the description apic already masked them into), then cookies with
 * their scope and expiry, never their values.
 */
export function sessionItems(vars: Record<string, string>, cookies: CookieInfo[], redact: boolean): SessionItem[] {
  const items: SessionItem[] = [];
  for (const name of Object.keys(vars).sort()) {
    const token = isTokenKey(name);
    items.push({ name, shown: token ? vars[name] : redact ? "***" : vars[name], kind: token ? "token" : "value" });
  }
  for (const c of cookies) {
    const when = c.expires ? `expires ${c.expires}` : "until cleared";
    items.push({ name: c.name, shown: `*** (${c.domain}${c.path} · ${when})`, kind: "cookie" });
  }
  return items;
}
