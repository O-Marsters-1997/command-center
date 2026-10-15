export type Glyph =
  | "failed" | "attention" | "running" | "pending" | "checking" | "ready" | "blocked" | "done";

export type State =
  | "blocked" | "ready" | "queued" | "running" | "failed" | "cut_failed" | "push_pending"
  | "checking" | "needs_you" | "push_failed" | "review_me" | "merged" | "pr_closed_unmerged"
  | "base_gone" | "cancelled" | "base_moved" | "ci_failed" | "refresh_conflicted"
  | "conflicts_with_main" | "verification_failed" | "waiting_on_producer_deploy" | "awaiting_input";

export interface Row {
  url: string;
  ref: string;
  title: string;
  state: State;
  glyph: Glyph;
  alive: boolean;
  blocking: string[] | null;
  after: string[] | null;
  reason: string;
  elapsed: string;
  pr_number: number;
  session_path: string;
}

export interface Group {
  root: Row | null;
  children: Row[];
}
