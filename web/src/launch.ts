// Mirrors internal/web/view's candidate json tags verbatim.
export interface Candidate {
  url: string;
  ref: string;
  title: string;
  repo: string;
  feature: string;
  label: string;
  reason: string;
  base: string;
  base_verdict: string;
  prompt_hash: string;
  blocked_by: string[] | null;
}

export const NOW = "now";
export const ON_UNLOCK = "on unlock";
export const REFUSED = "refused";

export function initialTicked(candidates: Candidate[]): Set<string> {
  return new Set(candidates.filter((c) => c.label !== REFUSED).map((c) => c.url));
}

export function dependentsOf(candidates: Candidate[], ticked: Set<string>, url: string): Candidate[] {
  return candidates.filter((c) => c.url !== url && ticked.has(c.url) && (c.blocked_by || []).includes(url));
}

export function missingBlockersOf(candidates: Candidate[], ticked: Set<string>, url: string): Candidate[] {
  const byURL = new Map(candidates.map((c) => [c.url, c]));
  const target = byURL.get(url);
  const blockers = (target?.blocked_by || []).filter((b) => byURL.has(b));
  return blockers.filter((b) => !ticked.has(b)).map((b) => byURL.get(b) as Candidate);
}

export function columnsFor(candidates: Candidate[]): Map<string, number> {
  const byURL = new Map(candidates.map((c) => [c.url, c]));
  const col = new Map<string, number>();

  function depth(url: string): number {
    const cached = col.get(url);
    if (cached !== undefined) return cached;
    col.set(url, 0);
    const blockers = (byURL.get(url)?.blocked_by || []).filter((b) => byURL.has(b));
    const d = blockers.length === 0 ? 0 : 1 + Math.max(...blockers.map(depth));
    col.set(url, d);
    return d;
  }

  for (const c of candidates) depth(c.url);
  return col;
}

export interface Sections {
  now: Candidate[];
  onUnlock: Candidate[];
  refused: Candidate[];
}

export function sectionsFor(candidates: Candidate[]): Sections {
  return {
    now: candidates.filter((c) => c.label === NOW),
    onUnlock: candidates.filter((c) => c.label === ON_UNLOCK),
    refused: candidates.filter((c) => c.label === REFUSED),
  };
}

export function cancelHref(feature: string): string {
  return feature ? `/f/${encodeURIComponent(feature)}` : "/";
}
