// Renders `apic run --json` results and `apic describe --json` as the
// HTML the response panel shows. Pure and deterministic, so the output
// for a fixture is unit-tested under node; the panel wraps it with the
// CSP, the stylesheet and the script.
import type { Description, RunResult } from "./types";

/** Bodies above this many characters are shown truncated, with a way to open the whole thing. */
export const BODY_LIMIT = 1_000_000;
/** How much of an oversized body is shown inline. */
export const BODY_PREVIEW = 64 * 1024;

export interface RenderOptions {
  /** The run used --redact: say so, since the masked values look like data. */
  redact?: boolean;
  /** A heading above a flow, for example the file that was run. */
  title?: string;
}

export function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

/** The body as text: pretty-printed JSON when it is JSON, else as given. */
export function bodyText(body: unknown): { text: string; json: boolean } {
  if (body === undefined || body === null) {
    return { text: "", json: false };
  }
  if (typeof body === "string") {
    const t = body.trim();
    if ((t.startsWith("{") || t.startsWith("[")) && t.length < BODY_LIMIT) {
      try {
        return { text: JSON.stringify(JSON.parse(t), null, 2), json: true };
      } catch {
        // not JSON after all
      }
    }
    return { text: body, json: false };
  }
  return { text: JSON.stringify(body, null, 2), json: true };
}

/** The body exactly as the API sent it, for saving: JSON compact or the string. */
export function rawBody(body: unknown): string {
  if (body === undefined || body === null) {
    return "";
  }
  return typeof body === "string" ? body : JSON.stringify(body);
}

/** Wraps JSON tokens in spans with the classes the stylesheet colours. */
export function highlightJson(pretty: string): string {
  const token = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g;
  let out = "";
  let last = 0;
  for (let m = token.exec(pretty); m; m = token.exec(pretty)) {
    out += escapeHtml(pretty.slice(last, m.index));
    if (m[1] !== undefined) {
      out += m[2] !== undefined ? `<span class="key">${escapeHtml(m[1])}</span>${m[2]}` : `<span class="str">${escapeHtml(m[1])}</span>`;
    } else if (m[3] !== undefined) {
      out += `<span class="lit">${m[3]}</span>`;
    } else {
      out += `<span class="num">${m[4]}</span>`;
    }
    last = m.index + m[0].length;
  }
  return out + escapeHtml(pretty.slice(last));
}

export function statusClass(code: number): string {
  if (code < 300) {
    return "s2";
  }
  if (code < 400) {
    return "s3";
  }
  return "s4";
}

export function size(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} kB`;
  }
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function headerLines(headers: Record<string, string> | undefined): string {
  if (!headers) {
    return "";
  }
  return Object.keys(headers)
    .sort()
    .map((k) => `${escapeHtml(k)}: ${escapeHtml(headers[k])}`)
    .join("\n");
}

function requestName(r: RunResult): string {
  return r.request.name ?? `${r.request.file}:${r.request.line}`;
}

/** The one-line reason a result failed, as the CLI's summary shows it. */
export function firstProblem(r: RunResult): string {
  for (const a of r.asserts ?? []) {
    if (a.error) {
      return `${a.expr} (${a.error})`;
    }
    if (!a.pass) {
      return `${a.expr} (actual: ${a.actual ?? ""})`;
    }
  }
  return r.errors?.[0] ?? "";
}

/** One result, as a block. `index` identifies it to the panel's save and open-raw buttons. */
export function renderResult(r: RunResult, index: number, collapsible: boolean): string {
  const parts: string[] = [];
  const deps = r.ran_first ?? [];
  for (const dep of deps) {
    parts.push(`<div class="dep">${renderResult(dep, -1, true)}<p class="dim">↳ ran ${escapeHtml(requestName(dep))} first (# @ref)</p></div>`);
  }
  const method = escapeHtml(r.request.method);
  const url = escapeHtml(r.request.url);
  let status: string;
  if (r.response) {
    const res = r.response;
    const attempts = r.attempts && r.attempts > 1 ? ` <span class="dim">· ${r.attempts} attempts</span>` : "";
    status = `<span class="status ${statusClass(res.status)}">${res.status} ${escapeHtml(res.status_text)}</span> <span class="dim">· ${res.duration_ms} ms · ${size(res.size)}</span>${attempts}`;
  } else {
    status = `<span class="status s4">not sent</span>`;
  }
  const summary = `<span class="mark ${r.ok ? "pass" : "fail"}">${r.ok ? "✓" : "✗"}</span> <span class="method ${method}">${method}</span> <span class="url">${url}</span> ${status}`;

  const body: string[] = [];
  const requestDetail = [headerLines(r.request.headers), r.request.body ? `\n${r.request.body}` : ""].filter(Boolean).join("\n");
  if (requestDetail) {
    body.push(`<details class="headers"><summary>Request</summary><pre>${escapeHtml(requestDetail)}</pre></details>`);
  }
  if (r.response) {
    const count = Object.keys(r.response.headers ?? {}).length;
    body.push(`<details class="headers"><summary>Response headers (${count})</summary><pre>${headerLines(r.response.headers)}</pre></details>`);
    const { text, json } = bodyText(r.response.body);
    if (text) {
      const truncated = text.length > BODY_LIMIT;
      const shown = truncated ? text.slice(0, BODY_PREVIEW) : text;
      const pretty = json ? highlightJson(shown) : escapeHtml(shown);
      const controls =
        index >= 0
          ? `<div class="toolbar">${json ? `<button data-action="toggle-raw" data-index="${index}">Raw</button>` : ""}<button data-action="save" data-index="${index}">Save body…</button>${truncated ? `<button data-action="open-raw" data-index="${index}">Open the whole body (${size(text.length)})</button>` : ""}</div>`
          : "";
      const raw = json && index >= 0 ? `<pre class="body raw hidden" data-index="${index}">${escapeHtml(rawBody(r.response.body))}</pre>` : "";
      body.push(`<div class="body-wrap">${controls}<pre class="body pretty" data-index="${index}">${pretty}${truncated ? "\n…" : ""}</pre>${raw}</div>`);
    }
  }
  if (r.asserts && r.asserts.length > 0) {
    const items = r.asserts
      .map((a) => {
        const cls = a.pass ? "pass" : "fail";
        let detail = "";
        if (a.error) {
          detail = ` <span class="dim">${escapeHtml(a.error)}</span>`;
        } else if (!a.pass) {
          detail = ` <span class="dim">actual</span> <code>${escapeHtml(a.actual ?? "")}</code> <span class="dim">expected</span> <code>${escapeHtml(a.expected ?? "")}</code>`;
        }
        return `<li class="${cls}"><span class="mark ${cls}">${a.pass ? "✓" : "✗"}</span> <code>${escapeHtml(a.expr)}</code>${detail}</li>`;
      })
      .join("");
    body.push(`<ul class="checks">${items}</ul>`);
  }
  if (r.captures && Object.keys(r.captures).length > 0) {
    const items = Object.keys(r.captures)
      .sort()
      .map((k) => `<li><span class="capture">↳ ${escapeHtml(k)}</span> = <code>${escapeHtml(r.captures![k])}</code></li>`)
      .join("");
    body.push(`<ul class="captures">${items}</ul>`);
  }
  if (r.errors && r.errors.length > 0) {
    body.push(`<ul class="errors">${r.errors.map((e) => `<li><span class="mark fail">✗</span> ${escapeHtml(e)}</li>`).join("")}</ul>`);
  }
  const inner = body.join("");
  parts.push(
    collapsible
      ? `<details class="result" open><summary>${summary}</summary>${inner}</details>`
      : `<section class="result"><h2>${summary}</h2>${inner}</section>`,
  );
  return parts.join("");
}

/** The whole panel body for a run: one result, or a flow with its summary. */
export function renderRun(results: RunResult[], opts: RenderOptions = {}): string {
  const parts: string[] = [];
  if (opts.redact) {
    parts.push(`<p class="badge">redacted</p>`);
  }
  if (results.length === 0) {
    parts.push(`<p class="dim">Nothing was sent.</p>`);
    return `<div class="run">${parts.join("")}</div>`;
  }
  const flow = results.length > 1;
  if (flow) {
    const passed = results.filter((r) => r.ok).length;
    const failed = results.length - passed;
    const total = results.reduce((n, r) => n + (r.response?.duration_ms ?? 0), 0);
    const line = failed > 0 ? `<span class="fail">${failed} failed</span>, ${passed} passed` : `<span class="pass">${passed} passed</span>`;
    const rows = results
      .map((r) => {
        const status = r.response ? `<span class="status ${statusClass(r.response.status)}">${r.response.status}</span>` : `<span class="dim">—</span>`;
        const why = r.ok ? "" : ` <span class="dim">${escapeHtml(firstProblem(r))}</span>`;
        return `<tr><td><span class="mark ${r.ok ? "pass" : "fail"}">${r.ok ? "✓" : "✗"}</span></td><td>${escapeHtml(requestName(r))}</td><td>${status}</td><td class="dim">${r.response?.duration_ms ?? 0} ms</td><td>${why}</td></tr>`;
      })
      .join("");
    parts.push(
      `<section class="summary">${opts.title ? `<h1>${escapeHtml(opts.title)}</h1>` : ""}<table>${rows}</table><p>${line} <span class="dim">· ${results.length} requests · ${total} ms</span></p></section>`,
    );
  }
  results.forEach((r, i) => parts.push(renderResult(r, i, flow)));
  return `<div class="run">${parts.join("")}</div>`;
}

/** The panel body for `apic describe`: the request, its variables and their sources. */
export function renderDescription(d: Description): string {
  const parts: string[] = [];
  const method = escapeHtml(d.method);
  parts.push(`<section class="result"><h2><span class="method ${method}">${method}</span> <span class="url">${escapeHtml(d.url_template)}</span></h2>`);
  if (d.description) {
    parts.push(`<p>${escapeHtml(d.description)}</p>`);
  }
  parts.push(`<p class="dim">${escapeHtml(d.file)}:${d.line}</p>`);
  const ready = d.ready
    ? `<p class="badge pass">ready</p>`
    : `<p class="badge fail">not ready · missing ${d.variables
        .filter((v) => v.missing)
        .map((v) => `{{${escapeHtml(v.name)}}}`)
        .join(", ")}</p>`;
  parts.push(ready);
  if (d.auth) {
    parts.push(`<h3>auth</h3><pre>${escapeHtml(d.auth)} <span class="dim">(${escapeHtml(d.auth_source ?? "")})</span></pre>`);
  }
  if (Object.keys(d.headers ?? {}).length > 0) {
    parts.push(`<h3>headers</h3><pre>${headerLines(d.headers)}</pre>`);
  }
  if (d.body) {
    parts.push(`<h3>body</h3><pre>${escapeHtml(d.body)}</pre>`);
  } else if (d.body_file) {
    parts.push(`<h3>body</h3><pre>&lt; ${escapeHtml(d.body_file)}</pre>`);
  }
  if (d.variables.length > 0) {
    const rows = d.variables
      .map((v) => {
        if (v.missing) {
          const hint = v.ref_runs
            ? `captured by ${escapeHtml(v.captured_by ?? "")}, which # @ref runs first`
            : v.captured_by
              ? `captured by ${escapeHtml(v.captured_by)} — run it first`
              : `pass --var ${escapeHtml(v.name)}=…`;
          return `<tr class="${v.ref_runs ? "" : "fail"}"><td><span class="mark ${v.ref_runs ? "warn" : "fail"}">${v.ref_runs ? "○" : "✗"}</span> ${escapeHtml(v.name)}</td><td class="dim">missing</td><td class="dim">${hint}</td></tr>`;
        }
        const value = v.secret ? "***" : escapeHtml(v.value ?? "");
        return `<tr><td><span class="mark pass">✓</span> ${escapeHtml(v.name)}</td><td><code>${value}</code></td><td class="dim">${escapeHtml(v.source)}</td></tr>`;
      })
      .join("");
    parts.push(`<h3>variables</h3><table class="vars">${rows}</table>`);
  }
  const list = (title: string, items: string[] | undefined, prefix = "") => {
    if (items && items.length > 0) {
      parts.push(`<h3>${title}</h3><ul>${items.map((i) => `<li>${prefix}${escapeHtml(i)}</li>`).join("")}</ul>`);
    }
  };
  list("steps", d.steps);
  list("refs", d.refs, "# @ref ");
  list("captures", d.captures, "↳ ");
  list("asserts", d.asserts);
  parts.push(`</section>`);
  return `<div class="describe">${parts.join("")}</div>`;
}
