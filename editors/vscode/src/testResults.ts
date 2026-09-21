// From the cucumber JSON `apic test --json` prints to per-scenario
// outcomes the Test Explorer can show: status, duration, the failing
// step and its message. Pure: unit-tested under node.

export interface CukeStep {
  keyword: string;
  name: string;
  line: number;
  result: { status: string; duration?: number; error_message?: string };
}

export interface CukeElement {
  keyword: string;
  name: string;
  line: number;
  type: string;
  steps: CukeStep[];
}

export interface CukeFeature {
  uri: string;
  name: string;
  elements: CukeElement[];
}

export interface StepOutcome {
  line: number;
  keyword: string;
  name: string;
  status: string;
  error?: string;
  durationMs: number;
}

export interface ScenarioOutcome {
  /** As apic printed it: absolute, or relative to the project root. */
  uri: string;
  /** The scenario's line, or the example row's for an outline. */
  line: number;
  name: string;
  status: "passed" | "failed" | "skipped";
  durationMs: number;
  steps: StepOutcome[];
  /** The first step that failed, was undefined or otherwise broke the scenario. */
  failure?: { line: number; message: string };
}

/** Parses the command's stdout; undefined when it is not cucumber JSON. */
export function parseCucumber(text: string): CukeFeature[] | undefined {
  if (!text.trim()) {
    return undefined;
  }
  try {
    const value = JSON.parse(text) as unknown;
    return Array.isArray(value) ? (value as CukeFeature[]) : undefined;
  } catch {
    return undefined;
  }
}

const undefinedHint = "Run `apic test --steps` for the vocabulary, or declare the phrase on a request with `# @step`.";

/** The message for a step that did not pass. */
export function stepMessage(st: StepOutcome): string {
  switch (st.status) {
    case "failed":
      return st.error ?? `${st.keyword.trim()} ${st.name} failed`;
    case "undefined":
      return `Undefined step: ${st.keyword.trim()} ${st.name}\n${undefinedHint}`;
    case "ambiguous":
      return `Ambiguous step: ${st.keyword.trim()} ${st.name} matches more than one phrase.`;
    case "pending":
      return `Pending step: ${st.keyword.trim()} ${st.name}`;
    default:
      return `${st.keyword.trim()} ${st.name}: ${st.status}`;
  }
}

/**
 * One outcome per scenario (per example row for an outline). godog emits
 * a background element before each scenario it applies to; its steps are
 * folded into that scenario.
 */
export function scenarioOutcomes(features: readonly CukeFeature[]): ScenarioOutcome[] {
  const out: ScenarioOutcome[] = [];
  for (const f of features) {
    let background: StepOutcome[] = [];
    for (const el of f.elements ?? []) {
      const steps = (el.steps ?? []).map((st) => ({
        line: st.line,
        keyword: st.keyword ?? "",
        name: st.name,
        status: st.result?.status ?? "skipped",
        error: st.result?.error_message,
        durationMs: (st.result?.duration ?? 0) / 1e6,
      }));
      if (el.type === "background") {
        background = steps;
        continue;
      }
      if (el.type !== "scenario") {
        continue;
      }
      const all = [...background, ...steps];
      background = [];
      const sc: ScenarioOutcome = { uri: f.uri, line: el.line, name: el.name, status: "passed", durationMs: 0, steps: all };
      let ran = false;
      for (const st of all) {
        sc.durationMs += st.durationMs;
        if (st.status === "passed") {
          ran = true;
        } else if (st.status !== "skipped" && !sc.failure) {
          sc.failure = { line: st.line, message: stepMessage(st) };
        }
      }
      if (sc.failure) {
        sc.status = "failed";
      } else if (!ran) {
        sc.status = "skipped";
      }
      out.push(sc);
    }
  }
  return out;
}

const marks: Record<string, string> = { passed: "✓", failed: "✗", undefined: "?", ambiguous: "?", pending: "…", skipped: "-" };

/** The lines the run's output shows for a scenario, step by step. */
export function outcomeLines(sc: ScenarioOutcome): string[] {
  const lines = [`${marks[sc.status] ?? " "} ${sc.name} (${Math.round(sc.durationMs)} ms)`];
  for (const st of sc.steps) {
    lines.push(`    ${marks[st.status] ?? " "} ${st.keyword.trim()} ${st.name}`);
    if (st.status !== "passed" && st.status !== "skipped") {
      for (const l of stepMessage(st).split("\n")) {
        lines.push(`        ${l}`);
      }
    }
  }
  return lines;
}

/** Whether a path apic printed names a feature file: equal, or the same file under a root. */
export function sameFile(printed: string, root: string, fsPath: string): boolean {
  const norm = (p: string) => p.split("\\").join("/").replace(/\/+$/, "");
  const a = norm(printed);
  const b = norm(fsPath);
  if (a === b) {
    return true;
  }
  const rel = a.replace(/^\.\//, "");
  return `${norm(root)}/${rel}` === b;
}
