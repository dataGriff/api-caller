// A light reading of Gherkin, enough to build the Test Explorer tree:
// feature, background, scenarios, outlines with their example rows, tags
// and step lines. apic's own parser (godog's) runs the features; this
// only needs to know where things are. Pure: unit-tested under node.

export interface Step {
  keyword: string;
  text: string;
  /** 1-based. */
  line: number;
}

export interface ExampleRow {
  line: number;
  cells: string[];
}

export interface Examples {
  name: string;
  line: number;
  tags: string[];
  header: string[];
  rows: ExampleRow[];
}

export interface Scenario {
  keyword: "Scenario" | "Scenario Outline";
  name: string;
  line: number;
  /** The scenario's own tags and the feature's. */
  tags: string[];
  steps: Step[];
  examples: Examples[];
}

export interface Feature {
  name: string;
  line: number;
  tags: string[];
  background?: { line: number; steps: Step[] };
  scenarios: Scenario[];
}

const keywordRe = /^\s*(Feature|Background|Scenario Outline|Scenario Template|Scenario|Example|Examples|Scenarios|Rule):\s*(.*)$/;
const stepRe = /^\s*(Given|When|Then|And|But|\*)\s+(.*?)\s*$/;
const tagRe = /^\s*@\S/;
const docStringRe = /^\s*("""|```)/;

/** Parses a feature file; undefined when it has no `Feature:` line. */
export function parseFeature(text: string): Feature | undefined {
  let feature: Feature | undefined;
  let scenario: Scenario | undefined;
  let examples: Examples | undefined;
  let background: { line: number; steps: Step[] } | undefined;
  let tags: string[] = [];
  let inDocString: string | undefined;
  const lines = text.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const line = i + 1;
    const doc = docStringRe.exec(raw);
    if (doc) {
      inDocString = inDocString === doc[1] ? undefined : (inDocString ?? doc[1]);
      continue;
    }
    if (inDocString) {
      continue;
    }
    const trimmed = raw.trim();
    if (trimmed === "" || trimmed.startsWith("#")) {
      continue;
    }
    if (tagRe.test(raw)) {
      tags.push(...trimmed.split(/\s+/).filter((t) => t.startsWith("@")));
      continue;
    }
    const kw = keywordRe.exec(raw);
    if (kw) {
      const name = kw[2].trim();
      switch (kw[1]) {
        case "Feature":
          feature = { name, line, tags, scenarios: [] };
          break;
        case "Background":
          background = { line, steps: [] };
          if (feature) {
            feature.background = background;
          }
          scenario = undefined;
          examples = undefined;
          break;
        case "Scenario":
        case "Example":
        case "Scenario Outline":
        case "Scenario Template":
          scenario = { keyword: kw[1] === "Scenario" || kw[1] === "Example" ? "Scenario" : "Scenario Outline", name, line, tags: [...(feature?.tags ?? []), ...tags], steps: [], examples: [] };
          feature?.scenarios.push(scenario);
          background = undefined;
          examples = undefined;
          break;
        case "Examples":
        case "Scenarios":
          examples = { name, line, tags, header: [], rows: [] };
          scenario?.examples.push(examples);
          break;
        case "Rule":
          scenario = undefined;
          examples = undefined;
          background = undefined;
          break;
      }
      tags = [];
      continue;
    }
    if (trimmed.startsWith("|")) {
      if (examples) {
        const cells = trimmed
          .slice(1, trimmed.endsWith("|") ? -1 : undefined)
          .split("|")
          .map((c) => c.trim());
        if (examples.header.length === 0) {
          examples.header = cells;
        } else {
          examples.rows.push({ line, cells });
        }
      }
      continue; // a step's data table otherwise
    }
    const st = stepRe.exec(raw);
    if (st) {
      const step = { keyword: st[1], text: st[2], line };
      if (examples) {
        continue;
      } else if (scenario) {
        scenario.steps.push(step);
      } else if (background) {
        background.steps.push(step);
      }
    }
    // Anything else is a description line.
  }
  return feature;
}

/** The label the tree shows for an example row: its cells, in order. */
export function rowLabel(row: ExampleRow, header: string[]): string {
  return row.cells.map((c, i) => (header[i] ? `${header[i]} = ${c}` : c)).join(", ");
}

/**
 * `test.paths` from an apic.yaml's text, or undefined when it is not set.
 * Only the shapes the schema allows are read: a block list or a flow
 * list of strings under `test:`.
 */
export function testPathsFromYaml(text: string): string[] | undefined {
  const lines = text.split(/\r?\n/);
  let inTest = false;
  let testIndent = 0;
  let inPaths = false;
  let pathsIndent = 0;
  const paths: string[] = [];
  const unquote = (s: string) => s.trim().replace(/^["']|["']$/g, "");
  for (const raw of lines) {
    const line = raw.replace(/\s+#.*$/, "").trimEnd();
    if (line.trim() === "" || line.trim().startsWith("#")) {
      continue;
    }
    const indent = line.length - line.trimStart().length;
    if (!inTest) {
      if (/^test:\s*$/.test(line)) {
        inTest = true;
        testIndent = indent;
      }
      continue;
    }
    if (indent <= testIndent && !inPaths) {
      return undefined; // test: ended without paths
    }
    if (!inPaths) {
      const m = /^\s*paths:\s*(.*)$/.exec(line);
      if (m && indent > testIndent) {
        if (m[1].startsWith("[")) {
          return m[1]
            .replace(/^\[|\]$/g, "")
            .split(",")
            .map(unquote)
            .filter(Boolean);
        }
        if (m[1].trim()) {
          return [unquote(m[1])];
        }
        inPaths = true;
        pathsIndent = indent;
      }
      continue;
    }
    const item = /^\s*-\s*(.*)$/.exec(line);
    if (item && indent > pathsIndent) {
      paths.push(unquote(item[1]));
    } else {
      break;
    }
  }
  return inPaths ? paths : undefined;
}

/**
 * The regular expression a `# @step` phrase matches, as apic builds it:
 * `{name}` matches a quoted string or one word, `"{name}"` the inside of
 * a quoted string.
 */
export function stepRegex(phrase: string): RegExp {
  const paramRe = /"\{[A-Za-z_][\w.-]*\}"|\{[A-Za-z_][\w.-]*\}/g;
  let out = "^";
  let last = 0;
  for (const m of phrase.trim().matchAll(paramRe)) {
    out += escapeRegExp(phrase.trim().slice(last, m.index));
    out += m[0].startsWith('"') ? '"([^"]*)"' : '("[^"]*"|\\S+)';
    last = m.index + m[0].length;
  }
  out += escapeRegExp(phrase.trim().slice(last)) + "$";
  return new RegExp(out);
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** One place a phrase is used. */
export interface StepUsage {
  uri: string;
  line: number;
  /** "Feature › Scenario", or "Feature › Background" for a step every scenario runs. */
  label: string;
  /** How many scenarios this usage runs in: 1, or the feature's count for a background step. */
  scenarios: number;
}

/** Where the scenarios of the parsed features use a `# @step` phrase. */
export function stepUsages(features: readonly { uri: string; feature: Feature }[], phrase: string): StepUsage[] {
  const re = stepRegex(phrase);
  const out: StepUsage[] = [];
  for (const { uri, feature } of features) {
    for (const st of feature.background?.steps ?? []) {
      if (re.test(st.text)) {
        out.push({ uri, line: st.line, label: `${feature.name} › Background`, scenarios: feature.scenarios.length });
      }
    }
    for (const sc of feature.scenarios) {
      const rows = sc.examples.reduce((n, e) => n + e.rows.length, 0);
      for (const st of sc.steps) {
        if (re.test(st.text) || (sc.keyword === "Scenario Outline" && outlineMatches(re, st.text, sc))) {
          out.push({ uri, line: st.line, label: `${feature.name} › ${sc.name}`, scenarios: Math.max(1, rows) });
          break;
        }
      }
    }
  }
  return out;
}

/** An outline step matches when it matches for any example row's substitution. */
function outlineMatches(re: RegExp, text: string, sc: Scenario): boolean {
  for (const ex of sc.examples) {
    for (const row of ex.rows) {
      let filled = text;
      ex.header.forEach((h, i) => {
        filled = filled.split(`<${h}>`).join(row.cells[i] ?? "");
      });
      if (re.test(filled)) {
        return true;
      }
    }
  }
  return false;
}
