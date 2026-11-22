export type Glyph =
  | "failed" | "attention" | "asked" | "running" | "pending" | "checking" | "ready" | "blocked" | "done";

export type State =
  | "blocked" | "ready" | "queued" | "running" | "failed" | "push_pending" | "awaiting_pr"
  | "checking" | "needs_you" | "push_failed" | "review_me" | "merged" | "withdrawn"
  | "cancelled" | "base_moved" | "ci_failed" | "refresh_conflicted"
  | "conflicts_with_main" | "verification_failed" | "waiting_on_producer_deploy" | "awaiting_input" | "push_refused" | "parked"
  | "verifying" | "waiting_to_verify";

export interface Row {
  url: string;
  ref: string;
  title: string;
  state: State;
  label: string;
  detail: string;
  glyph: Glyph;
  alive: boolean;
  blocking: string[] | null;
  after: string[] | null;
  reason: string;
  elapsed: string;
  pr_number: number;
  checks_passed: number;
  checks_total: number;
  session_path: string;
}

export interface Group {
  root: Row | null;
  children: Row[];
}
