// Mirrors internal/cc/server.go's candidate json tags verbatim -- the launch modal island's only
// view of the data (web/src/types.ts's own header comment records the same precedent for Row).
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

export const REFUSED = "refused";

// initialTicked selects every candidate the modal can launch -- everything but a candidate
// plan.Preview already refused (an active launch, a conflicted base), which the island never
// offers a checkbox for at all.
export function initialTicked(candidates: Candidate[]): Set<string> {
  return new Set(candidates.filter((c) => c.label !== REFUSED).map((c) => c.url));
}

// dependentsOf returns the still-ticked candidates that name url in their own blocked_by --
// unticking url while any of these remain ticked would confirm them "on unlock" for a blocker
// that never launches with them (CONTEXT.md § Launch modal).
export function dependentsOf(candidates: Candidate[], ticked: Set<string>, url: string): Candidate[] {
  return candidates.filter((c) => c.url !== url && ticked.has(c.url) && (c.blocked_by || []).includes(url));
}

// missingBlockersOf returns url's own blockers that are not currently ticked -- ticking url while
// any of these are missing would confirm url "on unlock" for a blocker outside the launch, the
// same closure break dependentsOf guards from the other direction.
export function missingBlockersOf(candidates: Candidate[], ticked: Set<string>, url: string): Candidate[] {
  const byURL = new Map(candidates.map((c) => [c.url, c]));
  const target = byURL.get(url);
  const blockers = (target?.blocked_by || []).filter((b) => byURL.has(b));
  return blockers.filter((b) => !ticked.has(b)).map((b) => byURL.get(b) as Candidate);
}

// columnsFor lays the ticked set out left to right by how deep each candidate sits in its own
// blocked_by chain, ignoring a blocker outside the set (a mid-untick set is not yet the closed,
// confirmed one).
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
