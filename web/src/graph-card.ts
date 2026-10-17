import type { Row } from "./types";

type Card = Pick<
  Row,
  "glyph" | "label" | "detail" | "after" | "pr_number" | "checks_passed" | "checks_total"
>;

export function cardTag(row: Pick<Row, "glyph" | "label">): string {
  return row.glyph === "failed" || row.glyph === "attention" ? row.label : "";
}

export function subline(row: Card): string {
  if (row.glyph === "blocked" && row.after?.length) return `after ${row.after.join(", ")}`;
  if (!row.pr_number) return row.detail || row.label;
  const checks = row.checks_total ? ` · ${row.checks_passed}/${row.checks_total} checks` : "";
  return `PR #${row.pr_number}${checks}`;
}

export function waveSubtitle(col: number, rows: Pick<Row, "glyph">[]): string {
  if (col > 0 && rows.every((r) => r.glyph === "blocked")) return `unlocks as wave ${col} merges`;
  const live = rows.filter((r) => r.glyph === "running" || r.glyph === "pending" || r.glyph === "checking");
  return `${live.length} in progress`;
}
