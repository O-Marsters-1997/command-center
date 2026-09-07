// Mirrors internal/cc/server.go's row/group json tags verbatim: GET /graph.json marshals
// []group directly, so this is the graph island's only view of the data too
// (docs/prds/prd-fleet-view.md § One derivation).

export interface Check {
  name: string;
  status: string;
  conclusion: string;
}

export interface LogFilterLink {
  label: string;
  path: string;
  active: boolean;
}

export interface LogPhase {
  skill: string;
  note: string;
  at: string;
  lines: string[];
}

export interface LogDetail {
  path: string;
  streaming: boolean;
  lines: number;
  phase_count: number;
  phases: LogPhase[] | null;
  result: string;
  filters: LogFilterLink[] | null;
  stream_path: string;
}

export interface Row {
  url: string;
  title: string;
  state: string;
  reason: string;
  tone: string;
  unattended: boolean;
  alive: boolean;
  verbs: string[];
  branch: string;
  pending_verbs: string[] | null;
  base: string;
  base_verdict: string;
  stack_depth: number;
  merge_order: number;
  warning: string;
  blocking: string[] | null;
  worktree: string;
  pr_number: number;
  pr_state: string;
  pgid: string;
  elapsed: string;
  elapsed_seconds: number;
  elapsed_percent: number;
  log_path: string;
  cancel_count: number;
  spend_tokens: number;
  spend_usd: number;
  spend_settled: boolean;
  baseline_sha: string;
  checks: Check[];
  draft: boolean;
  draft_reason: string;
  selected: boolean;
  checked: boolean;
  select_path: string;
  select_push: string;
  toggle_path: string;
  toggle_push: string;
  log: LogDetail;
}

export interface Group {
  root: Row | null;
  children: Row[];
}
