window.CC = (() => {
  const PROJECTS = [
    { id: "acme", name: "acme", hue: 1, repos: ["acme/web", "acme/api"] },
    { id: "job-scraper", name: "job-scraper", hue: 2, repos: ["olly/job-scraper"] },
    { id: "command-center", name: "command-center", hue: 3, repos: ["olly/command-center"] },
  ];

  const FEATURES = [
    { id: "billing", project: "acme", title: "Billing", launched: "2h ago" },
    { id: "test-abstractions", project: "job-scraper", title: "Testing abstractions", launched: "48m ago" },
    { id: "global-view", project: "command-center", title: "Global session view", launched: "25m ago" },
    { id: "usage-meters", project: "command-center", title: "Usage meters", launched: "yesterday", settled: true },
  ];

  const T = (o) => ({ updated: 3, ...o, key: `${o.repo}${o.ref}` });

  const TICKETS = [
    T({ feature: "billing", ref: "#1", repo: "acme/web", title: "Wire the config loader", s: "failed", reason: "Conflicts with main", detail: "cc-1-wire-the-config-loader no longer merges cleanly into main", pr: "#4", checks: [2, 2], spend: "$0.00", verbs: ["Resolve", "Refresh"], updated: 6, last: "Rebase needed: main moved 3 commits since the PR opened." }),
    T({ feature: "billing", ref: "#12", repo: "acme/api", title: "Retry the metrics endpoint", s: "attention", reason: "Ready for review", detail: "Every required check passed", pr: "#3", checks: [1, 1], spend: "$0.00", diff: [84, 12], verbs: ["Open PR"], updated: 11, last: "Added exponential backoff with jitter; retries cap at 5. Tests cover the 429 and 503 paths." }),
    T({ feature: "billing", ref: "#2", repo: "acme/web", title: "Wire the metrics endpoint", s: "running", reason: "Agent running", elapsed: 75, tokens: 2438, turns: 4, spend: "$0.03", verbs: ["Kill"], updated: 0, last: "Working on Wire the metrics endpoint" }),
    T({ feature: "billing", ref: "#6", repo: "acme/api", title: "Extract the rate limiter", s: "running", reason: "Agent running", elapsed: 75, tokens: 3593, turns: 6, spend: "$0.04", verbs: ["Kill"], updated: 0, last: "Moving the token bucket into internal/ratelimit and updating callers." }),
    T({ feature: "billing", ref: "#11", repo: "acme/api", title: "Add the config loader", s: "checking", reason: "Waiting on checks", pr: "#2", checks: [0, 1], spend: "$0.00", verbs: ["Re-run"], updated: 2, last: "PR opened; waiting on the required build check." }),
    T({ feature: "billing", ref: "#10", repo: "acme/web", title: "Retry the search index", s: "pending", reason: "Waiting to push", spend: "$0.01", updated: 4, last: "Finished with 2 commits, waiting to push." }),
    T({ feature: "billing", ref: "#3", repo: "acme/web", title: "Document the audit log", s: "blocked", blockedBy: ["acme/web#1", "acme/web#2"], reason: "Blocked", detail: "not yet merged", updated: 40 }),
    T({ feature: "billing", ref: "#4", repo: "acme/web", title: "Add the session store", s: "blocked", blockedBy: ["acme/web#2", "acme/web#3"], reason: "Blocked", detail: "not yet merged", updated: 40 }),
    T({ feature: "billing", ref: "#7", repo: "acme/web", title: "Document the health probe", s: "blocked", blockedBy: ["acme/web#2"], reason: "Blocked", detail: "no pull request yet", updated: 40 }),
    T({ feature: "billing", ref: "#8", repo: "acme/web", title: "Wire the feature flags", s: "blocked", blockedBy: ["acme/web#2"], reason: "Blocked", detail: "no pull request yet", updated: 40 }),
    T({ feature: "billing", ref: "#9", repo: "acme/web", title: "Add the webhook sender", s: "blocked", blockedBy: ["acme/web#7"], reason: "Blocked", detail: "no pull request yet", updated: 40 }),
    T({ feature: "billing", ref: "#13", repo: "acme/api", title: "Add the invoice PDF export", s: "ready", reason: "Ready to launch", detail: "No blockers", updated: 120 }),
    T({ feature: "billing", ref: "#5", repo: "acme/api", title: "Wire the export job", s: "done", reason: "Merged", pr: "#1", checks: [1, 1], spend: "$0.00", verbs: ["Remove worktree"], updated: 70, last: "Pull request merged." }),

    T({ feature: "test-abstractions", ref: "#43", repo: "olly/job-scraper", title: "Extract the transport recorder helper", s: "failed", reason: "2 checks failing", detail: "go test ./queue/... and lint failed", pr: "#19", checks: [4, 6], spend: "$0.21", diff: [212, 340], verbs: ["Re-run", "Open log"], updated: 2, last: "queuetest.Recorder now wraps the transport; 2 queue tests assert on the old field names." }),
    T({ feature: "test-abstractions", ref: "#42", repo: "olly/job-scraper", title: "Share the failing-store doubles", s: "attention", reason: "Ready for review", detail: "Every required check passed", pr: "#18", checks: [6, 6], spend: "$0.17", diff: [96, 288], verbs: ["Open PR"], updated: 8, last: "Moved four failing-store doubles into storetest; 11 files now import it. Net −192 lines." }),
    T({ feature: "test-abstractions", ref: "#41", repo: "olly/job-scraper", title: "Adopt RunGolden across source adapters", s: "running", reason: "Agent running", elapsed: 412, tokens: 18240, turns: 14, spend: "$0.32", verbs: ["Kill"], updated: 0, last: "ashby and workable converted; smartrecruiters next. Skipping recruitee (#45)." }),
    T({ feature: "test-abstractions", ref: "#46", repo: "olly/job-scraper", title: "Snapshot-test the digest email", s: "ready", reason: "Ready to launch", detail: "No blockers", updated: 50 }),
    T({ feature: "test-abstractions", ref: "#44", repo: "olly/job-scraper", title: "Fold handler tests into handlerstest", s: "blocked", blockedBy: ["olly/job-scraper#42"], reason: "Blocked", detail: "not yet merged", updated: 48 }),
    T({ feature: "test-abstractions", ref: "#45", repo: "olly/job-scraper", title: "Frontend test fixtures", s: "blocked", blockedBy: ["olly/job-scraper#41", "olly/job-scraper#43"], reason: "Blocked", detail: "not yet merged", updated: 48 }),

    T({ feature: "global-view", ref: "#88", repo: "olly/command-center", title: "Cross-project session index", s: "running", reason: "Agent running", elapsed: 1290, tokens: 31020, turns: 22, spend: "$0.58", verbs: ["Kill"], updated: 0, last: "Indexing sessions by project and feature; adding a sort on attention rank." }),
    T({ feature: "global-view", ref: "#89", repo: "olly/command-center", title: "Attention ordering for the sidebar", s: "checking", reason: "Waiting on checks", pr: "#31", checks: [1, 3], spend: "$0.22", diff: [140, 36], verbs: ["Re-run"], updated: 1, last: "PR opened; 2 of 3 required checks still running." }),
    T({ feature: "global-view", ref: "#92", repo: "olly/command-center", title: "Keyboard shortcut sheet", s: "ready", reason: "Ready to launch", detail: "No blockers", updated: 26 }),
    T({ feature: "global-view", ref: "#90", repo: "olly/command-center", title: "Settle sessions on merge", s: "blocked", blockedBy: ["olly/command-center#88"], reason: "Blocked", detail: "no pull request yet", updated: 25 }),
    T({ feature: "global-view", ref: "#91", repo: "olly/command-center", title: "Phone check-in layout", s: "blocked", blockedBy: ["olly/command-center#88", "olly/command-center#89"], reason: "Blocked", detail: "not yet merged", updated: 25 }),

    T({ feature: "usage-meters", ref: "#80", repo: "olly/command-center", title: "Five-hour window meter", s: "done", reason: "Merged", pr: "#27", checks: [3, 3], spend: "$0.14", updated: 900, last: "Pull request merged." }),
    T({ feature: "usage-meters", ref: "#81", repo: "olly/command-center", title: "Weekly limit meter", s: "done", reason: "Merged", pr: "#28", checks: [3, 3], spend: "$0.11", updated: 880, last: "Pull request merged." }),
  ];

  const LABEL = {
    failed: "Failed", attention: "Ready for review", running: "Running", checking: "Checking",
    pending: "Push pending", blocked: "Blocked", ready: "Ready to launch", done: "Merged",
  };
  const RANK = { failed: 0, attention: 1, running: 2, checking: 3, pending: 4, ready: 5, blocked: 6, done: 7 };
  const NEEDS_YOU = new Set(["failed", "attention"]);
  const ACTIVE = new Set(["running", "checking", "pending"]);

  const byKey = (k) => TICKETS.find((t) => t.key === k);
  const project = (id) => PROJECTS.find((p) => p.id === id);
  const feature = (id) => FEATURES.find((f) => f.id === id);
  const ticketsOf = (fid) => TICKETS.filter((t) => t.feature === fid);
  const projectOf = (t) => project(feature(t.feature).project);

  function depth(t, seen = new Set()) {
    if (!t.blockedBy || seen.has(t.key)) return 0;
    seen.add(t.key);
    return 1 + Math.max(...t.blockedBy.map((k) => depth(byKey(k), seen)));
  }

  function featureStats(fid) {
    const ts = ticketsOf(fid);
    const n = (pred) => ts.filter(pred).length;
    return {
      total: ts.length,
      merged: n((t) => t.s === "done"),
      needs: n((t) => NEEDS_YOU.has(t.s)),
      active: n((t) => ACTIVE.has(t.s)),
      running: n((t) => t.s === "running"),
      blocked: n((t) => t.s === "blocked"),
      ready: n((t) => t.s === "ready"),
      waves: 1 + Math.max(...ts.map((t) => depth(t))),
    };
  }

  function featureState(fid) {
    const s = featureStats(fid);
    if (s.merged === s.total) return "done";
    if (s.needs) return ticketsOf(fid).some((t) => t.s === "failed") ? "failed" : "attention";
    if (s.active) return "running";
    return "blocked";
  }

  const unlocks = (t) => TICKETS.filter((o) => o.blockedBy && o.blockedBy.includes(t.key));

  function fmtDur(s) {
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
    if (h) return `${h}h ${String(m).padStart(2, "0")}m`;
    return m ? `${m}m ${String(r).padStart(2, "0")}s` : `${r}s`;
  }
  function ago(min) {
    if (min <= 0) return "now";
    if (min < 60) return `${min}m`;
    if (min < 1440) return `${Math.floor(min / 60)}h`;
    return `${Math.floor(min / 1440)}d`;
  }

  const I = (d, cls = "icon") => `<svg class="${cls}" viewBox="0 0 24 24" aria-hidden="true">${d}</svg>`;
  const ICON = {
    search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
    inbox: '<path d="M3 13h5l1.5 3h5L16 13h5"/><path d="M5 5h14l2 8v6H3v-6l2-8Z"/>',
    play: '<circle cx="12" cy="12" r="9"/><path d="m10 9 5 3-5 3V9Z"/>',
    review: '<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M6 8.5v7M18 15.5V9a3 3 0 0 0-3-3h-3"/><path d="m14 4-2 2 2 2"/>',
    merged: '<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="12" r="2.5"/><path d="M6 8.5v7M8 7.5c2 3 5 4.5 7.5 4.5"/>',
    graph: '<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="12" r="2.5"/><path d="M8.5 6.5 15.5 11M8.5 17.5l7-4.5"/>',
    chart: '<path d="M3 3v18h18"/><path d="M8 16v-4M13 16V8M18 16v-6"/>',
    repo: '<path d="M4 5a2 2 0 0 1 2-2h12v16H6a2 2 0 0 0-2 2V5Z"/><path d="M4 19a2 2 0 0 1 2-2h12"/>',
    layers: '<path d="M12 3 3 8l9 5 9-5-9-5Z"/><path d="m3 13 9 5 9-5"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    chev: '<path d="m9 6 6 6-6 6"/>',
    down: '<path d="m6 9 6 6 6-6"/>',
    back: '<path d="m15 6-6 6 6 6"/>',
    x: '<path d="M18 6 6 18M6 6l12 12"/>',
    check: '<path d="m5 12 4.5 4.5L19 7"/>',
    term: '<path d="m5 17 5-5-5-5"/><path d="M12 19h7"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-1.8-.3 1.6 1.6 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.6 1.6 0 0 0-1-1.5 1.6 1.6 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.6 1.6 0 0 0 .3-1.8 1.6 1.6 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.6 1.6 0 0 0 1.5-1 1.6 1.6 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 1.8.3H9a1.6 1.6 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 1 1.5 1.6 1.6 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8V9a1.6 1.6 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1Z"/>',
    sidebar: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>',
    send: '<path d="M12 19V5M6 11l6-6 6 6"/>',
    lock: '<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
    bolt: '<path d="M13 2 4 14h7l-1 8 9-12h-7l1-8Z"/>',
    refresh: '<path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/>',
    menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
    more: '<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
    filter: '<path d="M4 5h16M7 12h10M10 19h4"/>',
    snooze: '<path d="M12 7v5l3 2"/><circle cx="12" cy="12" r="9"/>',
  };
  const icon = (name, cls) => I(ICON[name], cls);

  const glyph = (s, live) => `<span class="status${live && s === "running" ? " is-live" : ""}" data-s="${s}" role="img" aria-label="${LABEL[s]}"></span>`;
  const avatar = (p) => `<span class="avatar" data-hue="${p.hue}">${p.name[0]}</span>`;

  let toastTimer;
  function toast(msg, undo) {
    let el = document.getElementById("cc-toast");
    if (!el) {
      el = document.createElement("div");
      el.id = "cc-toast";
      el.className = "toast";
      el.setAttribute("role", "status");
      document.body.appendChild(el);
    }
    el.innerHTML = `<span>${msg}</span>${undo ? '<button type="button">Undo</button>' : ""}`;
    if (undo) el.querySelector("button").onclick = () => { undo(); el.classList.remove("is-on"); };
    el.classList.add("is-on");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.remove("is-on"), 3200);
  }

  function tick(fn) {
    setInterval(() => {
      TICKETS.forEach((t) => { if (t.s === "running") t.elapsed++; });
      document.querySelectorAll("[data-elapsed]").forEach((el) => { el.textContent = fmtDur(byKey(el.dataset.elapsed).elapsed); });
      if (fn) fn();
    }, 1000);
  }

  const typing = (e) => e.target.closest("input, textarea, select, [contenteditable]");

  return {
    PROJECTS, FEATURES, TICKETS, LABEL, RANK, NEEDS_YOU, ACTIVE,
    byKey, project, feature, ticketsOf, projectOf, depth, featureStats, featureState, unlocks,
    fmtDur, ago, icon, glyph, avatar, toast, tick, typing,
  };
})();

CC.REPOS = [
  { name: "acme/web", project: "acme", state: "ready", checks: { clone: "ok", squash: "ok", main: "ok", settings: "default" }, note: "No .command-centre.toml on main, so defaults are in use." },
  { name: "acme/api", project: "acme", state: "ready", checks: { clone: "ok", squash: "ok", main: "ok", settings: "ok" } },
  { name: "acme/merges", project: "acme", state: "refused", checks: { clone: "ok", squash: "fail", main: "skip", settings: "skip" }, reason: "Allows merge commits", fix: "Turn on squash-only merges in the repo's GitHub settings, then track it again." },
  { name: "acme/beta", project: "acme", state: "refused", checks: { clone: "ok", squash: "ok", main: "fail", settings: "skip" }, reason: "Default branch is master, not main", fix: "Rename the default branch to main, then track it again." },
  { name: "olly/job-scraper", project: "job-scraper", state: "ready", checks: { clone: "ok", squash: "ok", main: "ok", settings: "ok" } },
  { name: "olly/job-scraper-ui", project: "job-scraper", state: "refused", checks: { clone: "ok", squash: "ok", main: "ok", settings: "fail" }, reason: "Settings file doesn't parse", fix: ".command-centre.toml, line 3: expected “=” after key “base”." },
  { name: "olly/command-center", project: "command-center", state: "ready", checks: { clone: "ok", squash: "ok", main: "ok", settings: "ok" } },
  { name: "olly/notes-api", project: null, state: "ready", checks: { clone: "ok", squash: "ok", main: "ok", settings: "ok" } },
  { name: "olly/cc-agents", project: "command-center", state: "pending", checks: { clone: "run", squash: "wait", main: "wait", settings: "wait" } },
];

CC.GH_SEARCH = ["acme/web", "acme/api", "acme/beta", "acme/merges", "acme/infra", "olly/job-scraper", "olly/job-scraper-ui", "olly/command-center", "olly/cc-agents", "olly/dotfiles", "olly/portfolio"];

CC.CHECKS = [
  ["clone", "Clone"],
  ["squash", "Squash-only merges"],
  ["main", "Default branch is main"],
  ["settings", "Settings file"],
];

CC.MERGED = [
  { title: "Extract the health probe", feature: "Observability", project: "acme", day: "Sep 29", agent: 0.9, follow: 0.2 },
  { title: "Document the config loader", feature: "Observability", project: "acme", day: "Sep 30", agent: 0.5, follow: 0 },
  { title: "Extract the webhook sender", feature: "Observability", project: "acme", day: "Sep 30", agent: 1.6, follow: 0.9 },
  { title: "Extract the audit log", feature: "Observability", project: "acme", day: "Oct 1", agent: 0.8, follow: 0.1 },
  { title: "Add the config loader", feature: "Observability", project: "acme", day: "Oct 2", agent: 1.1, follow: 0 },
  { title: "Five-hour window meter", feature: "Usage meters", project: "command-center", day: "Oct 7", agent: 1.3, follow: 0.4 },
  { title: "Weekly limit meter", feature: "Usage meters", project: "command-center", day: "Oct 8", agent: 0.9, follow: 0 },
  { title: "Wire the export job", feature: "Billing", project: "acme", day: "Oct 9", agent: 0.7, follow: 0 },
];

CC.USAGE = { fiveHour: { pct: 38, resets: "in 2h 14m" }, weekly: { pct: 12, resets: "in 3d 4h" } };

CC.WASTE = { pct: 0.6, tickets: [{ title: "Retry the auth handshake", feature: "Observability", pct: 0.4 }, { title: "Cache the search facets", feature: "Billing", pct: 0.2 }] };

CC.REPO_FEATURES = {
  "acme/web": [{ f: "Billing", imported: true, also: ["acme/api"] }, { f: "Observability", imported: true }],
  "acme/api": [{ f: "Billing", imported: true, also: ["acme/web"] }],
  "olly/job-scraper": [{ f: "Testing abstractions", imported: true }],
  "olly/command-center": [{ f: "Global session view", imported: true }, { f: "Usage meters", imported: true }],
  "olly/notes-api": [{ f: "sync", imported: false }],
};

CC.IMPORT_ERROR = { feature: "search-v2", age: "14m ago", message: "#31 is blocked by acme/search#4, and acme/search isn't tracked." };

CC.TOKENS_DAILY = Array.from({ length: 30 }, (_, i) => {
  const d = new Date(2026, 8, 17 + i);
  const wave = Math.max(0, Math.sin(i / 3.1) + Math.sin(i / 1.7) * 0.5 + 0.9);
  const weekend = [0, 6].includes(d.getDay()) ? 0.25 : 1;
  const tin = Math.round((60 + wave * 150) * weekend) * 1000;
  return { day: d.toLocaleDateString("en-US", { month: "short", day: "numeric" }), tin, tout: Math.round(tin * (0.16 + (i % 5) * 0.012)) };
});
