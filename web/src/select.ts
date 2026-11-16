export interface PickedRow {
  readonly url: string;
  readonly feature: string;
  readonly armed: boolean;
}

export interface Summary {
  readonly count: number;
  readonly now: number;
  readonly arm: number;
  readonly scope: string;
}

export function toggled(selected: readonly string[], url: string): string[] {
  return selected.includes(url) ? selected.filter((u) => u !== url) : [...selected, url];
}

export function withAll(selected: readonly string[], urls: readonly string[]): string[] {
  return [...selected, ...urls.filter((u) => !selected.includes(u))];
}

export function without(selected: readonly string[], urls: readonly string[]): string[] {
  return selected.filter((u) => !urls.includes(u));
}

export function rangeBetween(order: readonly string[], from: string, to: string): string[] {
  const a = order.indexOf(from);
  const b = order.indexOf(to);
  if (a === -1 || b === -1) return b === -1 ? [] : [to];
  return order.slice(Math.min(a, b), Math.max(a, b) + 1);
}

export function stepCursor(count: number, current: number, step: 1 | -1): number {
  if (count === 0) return -1;
  if (current < 0) return step === 1 ? 0 : count - 1;
  return Math.min(count - 1, Math.max(0, current + step));
}

export function summarize(picks: readonly PickedRow[]): Summary {
  const arm = picks.filter((p) => p.armed).length;
  const features = [...new Set(picks.map((p) => p.feature))];
  const scope = features.length === 1 ? (features[0] ?? "") : `across ${features.length} features`;
  return { count: picks.length, now: picks.length - arm, arm, scope };
}

export function launchHref(urls: readonly string[]): string {
  const query = new URLSearchParams(urls.map((u) => ["ticket", u]));
  return `/launch?${query.toString()}`;
}
