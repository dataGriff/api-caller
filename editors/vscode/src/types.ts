// The shapes apic prints with --json, as documented in docs/cli.md. Keys
// are additive across releases; optional fields are the ones older
// releases omit.

/** One request from `apic list --json`. */
export interface ListEntry {
  id: string;
  name?: string;
  method: string;
  url: string;
  file: string;
  line: number;
  description?: string;
  captures?: string[];
  asserts?: number;
  steps?: string[];
  refs?: string[];
}

export interface ListOutput {
  root: string;
  requests: ListEntry[];
}

/** One finding from `apic validate --json`. Span fields arrived in 0.2. */
export interface ValidateDiagnostic {
  path: string;
  line: number;
  column?: number;
  end_line?: number;
  end_column?: number;
  severity: "error" | "warning";
  code?: string;
  message: string;
}

export interface ValidateOutput {
  ok: boolean;
  files: number;
  requests: number;
  diagnostics: ValidateDiagnostic[];
}

export interface AssertResult {
  expr: string;
  pass: boolean;
  actual?: string;
  expected?: string;
  error?: string;
}

/** One line of `apic run --json`. */
export interface RunResult {
  ok: boolean;
  attempts?: number;
  request: {
    name?: string;
    file: string;
    line: number;
    method: string;
    url: string;
    headers?: Record<string, string>;
    body?: string;
    auth?: string;
  };
  response?: {
    status: number;
    status_text: string;
    headers: Record<string, string>;
    body: unknown;
    duration_ms: number;
    size: number;
  };
  captures?: Record<string, string>;
  asserts?: AssertResult[];
  errors?: string[];
  /** Present in the MCP result only; the CLI flattens dependencies into their own lines. */
  ran_first?: RunResult[];
}

export interface VarInfo {
  name: string;
  value?: string;
  source: string;
  secret?: boolean;
  missing?: boolean;
  captured_by?: string;
  ref_runs?: boolean;
}

/** `apic describe --json`. */
export interface Description {
  name?: string;
  id: string;
  file: string;
  line: number;
  description?: string;
  method: string;
  url_template: string;
  url: string;
  headers: Record<string, string>;
  body?: string;
  body_file?: string;
  /** null when the request has no placeholders at all. */
  variables: VarInfo[] | null;
  captures?: string[];
  asserts?: string[];
  steps?: string[];
  refs?: string[];
  auth?: string;
  auth_source?: string;
  ready: boolean;
}

/** `apic env --json`. */
export interface EnvOutput {
  root: string;
  environments: string[];
  current: string;
  files: string[];
  variables: VarInfo[];
}

/** `apic curl --json`. */
export interface CurlOutput {
  id: string;
  command: string;
}
