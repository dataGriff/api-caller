// What to offer where in a request file: directives after `# @`, variables
// inside `{{`, selectors after `# @assert` and `# @capture x =`, and the
// keys of the last response after `body.$.`. Pure: the provider in
// completion.ts turns these into VS Code items, and the unit tests run
// under plain node. The values come from apic's JSON, never from parsing
// the file beyond the line under the cursor.
import type { RunResult, VarInfo } from "./types";

/** Where the cursor is, and the text it is completing. */
export type Context =
  | { kind: "directive"; prefix: string; start: number }
  | { kind: "variable"; prefix: string; start: number; closed: boolean }
  | { kind: "selector"; prefix: string; start: number; directive: "assert" | "capture" }
  | { kind: "operator"; prefix: string; start: number }
  | { kind: "auth"; prefix: string; start: number }
  | { kind: "ref"; prefix: string; start: number };

/** One suggestion, in editor-neutral form. */
export interface Item {
  label: string;
  /** What is inserted; a snippet when `snippet` is set. */
  insert: string;
  snippet?: boolean;
  detail?: string;
  documentation?: string;
  /** Sorts before items without one (or with a later one). */
  sortText?: string;
  kind: "directive" | "variable" | "selector" | "operator" | "keyword" | "request";
  /** Text to filter on when it differs from the label. */
  filterText?: string;
}

const comment = String.raw`^\s*(?:#|\/\/)\s*`;
const directiveRe = new RegExp(comment + String.raw`@([\w-]*)$`);
const selectorRe = new RegExp(comment + String.raw`@(assert\s+|capture\s+[\w.-]+\s*=\s*)(\S*)$`);
const operatorRe = new RegExp(comment + String.raw`@assert\s+\S+\s+(\S*)$`);
const authRe = new RegExp(comment + String.raw`@auth\s+(\S*)$`);
const refRe = new RegExp(comment + String.raw`@(?:ref|forceRef)\s+(\S*)$`);
const variableRe = /\{\{\s*([^{}]*)$/;

/**
 * The context at `column` (0-based, the cursor) on a line. The `start` is
 * the column the completion replaces from.
 */
export function contextAt(line: string, column: number, after = ""): Context | undefined {
  const before = line.slice(0, column);
  let m = directiveRe.exec(before);
  if (m) {
    return { kind: "directive", prefix: m[1], start: column - m[1].length - 1 };
  }
  m = variableRe.exec(before);
  if (m) {
    return { kind: "variable", prefix: m[1], start: column - m[1].length, closed: after.startsWith("}}") };
  }
  m = selectorRe.exec(before);
  if (m) {
    return { kind: "selector", prefix: m[2], start: column - m[2].length, directive: m[1].startsWith("assert") ? "assert" : "capture" };
  }
  m = operatorRe.exec(before);
  if (m) {
    return { kind: "operator", prefix: m[1], start: column - m[1].length };
  }
  m = authRe.exec(before);
  if (m) {
    return { kind: "auth", prefix: m[1], start: column - m[1].length };
  }
  m = refRe.exec(before);
  if (m) {
    return { kind: "ref", prefix: m[1], start: column - m[1].length };
  }
  return undefined;
}

/** The assertion operators, in the order the docs list them. */
export const OPERATORS = [
  "==", "!=", "<", "<=", ">", ">=", "contains", "startsWith", "endsWith", "matches", "exists", "not exists",
  "isString", "isNumber", "isInteger", "isBoolean", "isArray", "isObject", "isNull", "isEmpty", "not isEmpty",
  "length ==", "matchesSchema",
];

/** The auth types `# @auth` takes, with the options each starts with. */
export const AUTH_TYPES: Record<string, string> = {
  bearer: "bearer {{${1:token}}}",
  basic: "basic {{${1:user}}} {{${2:password}}}",
  apikey: "apikey {{${1:apiKey}}} header=${2:X-Api-Key}",
  digest: "digest {{${1:user}}} {{${2:password}}}",
  aws: "aws region=${1:eu-west-2}",
  oauth2: "oauth2 tokenUrl={{${1:tokenUrl}}} clientId={{${2:clientId}}} clientSecret={{${3:clientSecret}}}",
  exec: "exec ${1:command}",
  none: "none",
};

interface DirectiveInfo {
  body: string;
  doc: string;
}

/** Snippet bodies and one-line docs, from docs/format.md. */
const DIRECTIVES: Record<string, DirectiveInfo> = {
  name: { body: "name ${1:request-name}", doc: "Name used on the command line and by MCP." },
  description: { body: "description ${1:text}", doc: "One line shown by list and describe; defaults to the ### title." },
  capture: { body: "capture ${1:name} = ${2:body.$.}", doc: "After the response arrives, store the selected value as a variable for later requests and the session." },
  assert: { body: "assert ${1:status} ${2|==,!=,<,<=,>,>=,contains,startsWith,endsWith,matches,exists,not exists|} ${3:200}", doc: "Check the response. A failure sets ok: false and exit code 1." },
  auth: { body: "auth ${1|bearer,basic,apikey,digest,aws,oauth2,exec,none|} ", doc: "Attach credentials: none, bearer, basic, apikey, digest, aws, oauth2 or exec." },
  step: { body: "step ${1:phrase}", doc: "A Gherkin phrase that runs this request from a .feature file; {name} becomes a variable." },
  ref: { body: "ref ${1:login}", doc: "Run the named request first when this one is missing a variable, once per invocation." },
  forceRef: { body: "forceRef ${1:login}", doc: "Run the named request first every time this one runs." },
  "no-redirect": { body: "no-redirect", doc: "Do not follow 3xx redirects." },
  "no-session": { body: "no-session", doc: "Do not persist this request's captures." },
  "no-cookies": { body: "no-cookies", doc: "Send no cookies with this request and keep none it sets." },
  timeout: { body: "timeout ${1:10s}", doc: "Per-request timeout." },
  retry: { body: "retry ${1:5} ${2:1s}", doc: "Re-send until every assertion passes, up to N times, this long apart." },
  sleep: { body: "sleep ${1:1s}", doc: "Wait this long before sending, e.g. for a rate-limited API." },
  disabled: { body: "disabled", doc: "Skip this request when its file runs as a flow; running it by name still sends it." },
  note: { body: "note ${1:text}", doc: "Free text, accepted and ignored (REST Client compatibility)." },
  prompt: { body: "prompt ${1:name}", doc: "Accepted and ignored: apic never prompts. Pass the value with --var or an env file." },
};

/** One item per known directive; a directive newer than this table gets a plain body. */
export function directiveItems(known: readonly string[]): Item[] {
  return known.map((name, i) => {
    const info = DIRECTIVES[name];
    return {
      label: `@${name}`,
      insert: `@${info?.body ?? `${name} `}`,
      snippet: true,
      detail: "apic directive",
      documentation: info?.doc,
      sortText: String(i).padStart(2, "0"),
      kind: "directive",
    };
  });
}

/** The built-in placeholders, with what they produce. */
export const BUILTINS: { name: string; insert: string; doc: string }[] = [
  { name: "$uuid", insert: "$uuid", doc: "random UUID v4" },
  { name: "$guid", insert: "$guid", doc: "random UUID v4" },
  { name: "$timestamp", insert: "$timestamp", doc: "Unix seconds" },
  { name: "$isoTimestamp", insert: "$isoTimestamp", doc: "RFC 3339 UTC" },
  { name: "$datetime", insert: "$datetime ${1|rfc1123,iso8601,\"2006-01-02\"|}", doc: "formatted UTC time (Go layout for custom formats); an offset like `-1 d` may follow" },
  { name: "$localDatetime", insert: "$localDatetime ${1|rfc1123,iso8601,\"2006-01-02\"|}", doc: "formatted time in the local zone; an offset like `1 h` may follow" },
  { name: "$randomInt", insert: "$randomInt ${1:1} ${2:100}", doc: "random integer in [min, max)" },
  { name: "$random.integer", insert: "$random.integer(${1:1}, ${2:100})", doc: "random integer in [min, max) (JetBrains)" },
  { name: "$random.float", insert: "$random.float(${1:0}, ${2:1})", doc: "random float with three decimals (JetBrains)" },
  { name: "$random.alphabetic", insert: "$random.alphabetic(${1:10})", doc: "random letters (JetBrains)" },
  { name: "$random.alphanumeric", insert: "$random.alphanumeric(${1:10})", doc: "random letters and digits (JetBrains)" },
  { name: "$random.hexadecimal", insert: "$random.hexadecimal(${1:10})", doc: "random hex digits (JetBrains)" },
  { name: "$random.email", insert: "$random.email", doc: "<8 letters>@example.com (JetBrains)" },
  { name: "$random.uuid", insert: "$random.uuid", doc: "random UUID v4 (JetBrains)" },
  { name: "$projectRoot", insert: "$projectRoot", doc: "the project root, absolute" },
  { name: "$processEnv", insert: "$processEnv ${1:NAME}", doc: "shell environment variable" },
  { name: "$env", insert: "$env.${1:NAME}", doc: "shell environment variable" },
  { name: "$dotenv", insert: "$dotenv ${1:NAME}", doc: "value from .env" },
];

/** What the variable completion draws on. */
export interface VariableSources {
  /** `apic env --json` variables, in resolution order. */
  env?: VarInfo[];
  /** The session's captured values for the environment in effect. */
  session?: Record<string, string>;
  /** Named requests in the same file, for response references. */
  requests?: string[];
}

/**
 * Variables inside `{{`: the environment's (source as detail, secrets
 * masked), the session's captures, built-ins, and `<name>.response.…` for
 * the named requests of the file. `closed` says `}}` already follows the
 * cursor, so it is not inserted again.
 */
export function variableItems(src: VariableSources, closed: boolean): Item[] {
  const close = closed ? "" : "}}";
  const items: Item[] = [];
  const seen = new Set<string>();
  for (const v of src.env ?? []) {
    if (seen.has(v.name)) {
      continue;
    }
    seen.add(v.name);
    const value = v.secret ? "***" : (v.value ?? "");
    items.push({ label: v.name, insert: v.name + close, detail: v.source, documentation: v.missing ? "not set" : `= ${value}`, sortText: `0${v.name}`, kind: "variable" });
  }
  for (const [name, value] of Object.entries(src.session ?? {})) {
    if (seen.has(name) || name.startsWith("$")) {
      continue;
    }
    seen.add(name);
    items.push({ label: name, insert: name + close, detail: "session", documentation: `= ${value}`, sortText: `0${name}`, kind: "variable" });
  }
  for (const b of BUILTINS) {
    items.push({ label: b.name, insert: b.insert + close, snippet: true, detail: "built-in", documentation: b.doc, sortText: `1${b.name}`, kind: "keyword" });
  }
  for (const r of src.requests ?? []) {
    items.push({ label: `${r}.response.body.$`, insert: `${r}.response.body.$.\${1:path}${close}`, snippet: true, detail: "response reference", documentation: `The JSON body ${r} received earlier in this run.`, sortText: `2${r}`, kind: "request" });
    items.push({ label: `${r}.response.headers`, insert: `${r}.response.headers.\${1:name}${close}`, snippet: true, detail: "response reference", documentation: `A header ${r} received earlier in this run.`, sortText: `2${r}`, kind: "request" });
  }
  return items;
}

const SELECTORS: { label: string; insert: string; doc: string; snippet?: boolean }[] = [
  { label: "status", insert: "status", doc: "status code, e.g. 200" },
  { label: "statusText", insert: "statusText", doc: "e.g. OK" },
  { label: "header.", insert: "header.${1:content-type}", doc: "first value of a response header, case-insensitive", snippet: true },
  { label: "cookie.", insert: "cookie.${1:name}", doc: "value of a cookie the response set" },
  { label: "body", insert: "body", doc: "raw body" },
  { label: "body.$", insert: "body.$", doc: "whole body (must be JSON)" },
  { label: "body.$.", insert: "body.$.${1:path}", doc: "JSON path such as body.$.items[0].id or body.$.items.# (count)", snippet: true },
  { label: "duration", insert: "duration", doc: "round-trip time in milliseconds" },
];

/**
 * Selector items for `# @assert` and `# @capture x =`; after `body.$.` the
 * keys of the last response for the request, when one is known.
 */
export function selectorItems(prefix: string, lastBody?: unknown): Item[] {
  if (prefix.startsWith("body.$") && (prefix.length > "body.$".length || lastBody !== undefined)) {
    const keys = bodyPathItems(prefix, lastBody);
    if (keys.length > 0) {
      return keys;
    }
  }
  return SELECTORS.map((s, i) => ({ label: s.label, insert: s.insert, snippet: s.snippet, detail: "selector", documentation: s.doc, sortText: String(i).padStart(2, "0"), kind: "selector" }));
}

/**
 * The paths one level below what is typed, from a response body: keys of
 * an object, `#` and `[0]` for an array. `prefix` is the whole selector,
 * `body.$.items.`.
 */
export function bodyPathItems(prefix: string, body: unknown): Item[] {
  if (body === undefined || !prefix.startsWith("body.$")) {
    return [];
  }
  // A `[` or `[12` being typed is an index in progress: the items offered
  // are those of the array itself, and they replace what was typed.
  const typed = prefix.slice("body.$".length);
  const indexing = /\[\d*$/.test(typed);
  const rest = indexing ? typed.replace(/\[\d*$/, "") : typed;
  // Otherwise the typed part up to the last separator is the path to
  // descend; the remainder filters (VS Code does that with the item's label).
  const cut = Math.max(rest.lastIndexOf("."), rest.lastIndexOf("["));
  const path = indexing ? rest : cut >= 0 ? rest.slice(0, cut + 1) : "";
  const node = descend(body, path);
  if (node === undefined) {
    return [];
  }
  const base = `body.$${path}`;
  const items: Item[] = [];
  if (Array.isArray(node)) {
    const sep = base.endsWith(".") ? base : `${base}.`;
    items.push({ label: `${sep}#`, insert: `${sep}#`, detail: "count", documentation: `${node.length} elements`, kind: "selector", sortText: "0" });
    if (node.length > 0) {
      const at = base.replace(/\.$/, "");
      items.push({ label: `${at}[0]`, insert: `${at}[0]`, detail: describe(node[0]), kind: "selector", sortText: "1" });
    }
    return items;
  }
  if (node && typeof node === "object") {
    const sep = base.endsWith(".") ? base : `${base}.`;
    for (const [k, v] of Object.entries(node as Record<string, unknown>)) {
      const key = /^[A-Za-z_][\w-]*$/.test(k) ? `${sep}${k}` : `${sep.replace(/\.$/, "")}["${k}"]`;
      items.push({ label: key, insert: key, detail: describe(v), kind: "selector", sortText: `2${k}` });
    }
  }
  return items;
}

/** Follows `.a.b[0].` through a JSON value; undefined when the path does not exist. */
function descend(root: unknown, path: string): unknown {
  let node: unknown = root;
  for (const m of path.matchAll(/\.?([^.[\]]+)|\[(\d+)\]|\["([^"]*)"\]/g)) {
    const key = m[1] ?? m[3];
    const index = m[2];
    if (index !== undefined) {
      node = Array.isArray(node) ? node[Number(index)] : undefined;
    } else if (key !== undefined && key !== "") {
      node = node && typeof node === "object" && !Array.isArray(node) ? (node as Record<string, unknown>)[key] : undefined;
    }
    if (node === undefined) {
      return undefined;
    }
  }
  return node;
}

function describe(v: unknown): string {
  if (v === null) {
    return "null";
  }
  if (Array.isArray(v)) {
    return `array of ${v.length}`;
  }
  if (typeof v === "object") {
    return "object";
  }
  const s = JSON.stringify(v);
  return s.length > 40 ? `${s.slice(0, 37)}…` : s;
}

/** The parsed JSON body of the last result for a request name, when it was JSON: the latest when it ran more than once. */
export function lastBodyFor(results: readonly RunResult[], name: string | undefined): unknown {
  if (!name) {
    return undefined;
  }
  for (let i = results.length - 1; i >= 0; i--) {
    const r = results[i];
    if (r.request.name === name && r.response && r.response.body !== null && typeof r.response.body === "object") {
      return r.response.body;
    }
  }
  return undefined;
}

/** Items for the assertion operators. */
export function operatorItems(): Item[] {
  return OPERATORS.map((op, i) => ({ label: op, insert: op, detail: "operator", sortText: String(i).padStart(2, "0"), kind: "operator" }));
}

/** Items for the auth types, each with the options it starts with. */
export function authItems(): Item[] {
  return Object.entries(AUTH_TYPES).map(([name, body], i) => ({ label: name, insert: body, snippet: true, detail: "# @auth type", sortText: String(i).padStart(2, "0"), kind: "keyword" }));
}

/** Items for the request names `# @ref` can name. */
export function refItems(names: readonly string[]): Item[] {
  return names.map((n) => ({ label: n, insert: n, detail: "request", kind: "request" }));
}
