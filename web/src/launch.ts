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

const NOW = "now";
const ON_UNLOCK = "on unlock";
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
  return blockers.flatMap((b) => {
    const candidate = byURL.get(b);
    return candidate && !ticked.has(b) ? [candidate] : [];
  });
}

type ToggleResult = { ticked: Set<string> } | { refusal: string };

function names(candidates: Candidate[]): string {
  return candidates.map((c) => `${c.ref} ${c.title}`).join(", ");
}

export function toggleResult(candidates: Candidate[], ticked: Set<string>, url: string): ToggleResult {
  const ref = candidates.find((c) => c.url === url)?.ref ?? url;
  if (!ticked.has(url)) {
    const missing = missingBlockersOf(candidates, ticked, url);
    if (missing.length > 0) {
      return { refusal: `${ref} is blocked by ${names(missing)} -- tick it first` };
    }
    return { ticked: new Set(ticked).add(url) };
  }
  const dependents = dependentsOf(candidates, ticked, url);
  if (dependents.length > 0) {
    return { refusal: `${names(dependents)} depends on ${ref} -- clear it first` };
  }
  const next = new Set(ticked);
  next.delete(url);
  return { ticked: next };
}

export function launchLabel(candidates: Candidate[], ticked: Set<string>): string {
  const chosen = candidates.filter((c) => ticked.has(c.url));
  const now = chosen.filter((c) => c.label === NOW).length;
  const arm = chosen.filter((c) => c.label === ON_UNLOCK).length;
  if (now > 0 && arm > 0) return `Launch ${now} now · arm ${arm}`;
  if (now > 0) return `Launch ${now} now`;
  if (arm > 0) return `Arm ${arm}`;
  return "Select tickets to launch";
}

export function whenLabel(c: Candidate, candidates: Candidate[]): string {
  if (c.label !== ON_UNLOCK) return c.repo;
  const refs = new Map(candidates.map((other) => [other.url, other.ref]));
  const blockers = (c.blocked_by || []).map((url) => refs.get(url) ?? `#${url.split("/").pop()}`);
  return `after ${blockers.join(", ")}`;
}

interface Sections {
  now: Candidate[];
  onUnlock: Candidate[];
  refused: Candidate[];
}

export function sectionsFor(candidates: Candidate[]): Sections {
  return {
    now: candidates.filter((c) => c.label === NOW),
    onUnlock: candidates.filter((c) => c.label === ON_UNLOCK),
    refused: candidates.filter((c) => c.label !== NOW && c.label !== ON_UNLOCK),
  };
}

export function cancelHref(feature: string): string {
  return feature ? `/f/${encodeURIComponent(feature)}` : "/";
}
