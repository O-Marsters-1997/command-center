(() => {
  const { TICKETS, FEATURES, PROJECTS, NEEDS_YOU, ACTIVE, byKey, feature, project, projectOf, ticketsOf, featureStats, icon, glyph, avatar, toast } = CC;
  const { st, pages, topbar, groupedBoard } = APP;
  const main = () => APP.main;

  const scoped = (t) => st.scope === "all" || projectOf(t).id === st.scope;
  const scopeChip = () => {
    const p = project(st.scope);
    return p ? `<button class="chip" type="button" data-scope="all" title="Show all projects">${avatar(p)}${p.name}${icon("x", "icon icon-sm")}</button>` : "";
  };

  const TFILTERS = [
    ["all", "All", () => true],
    ["needs", "Needs you", (t) => NEEDS_YOU.has(t.s)],
    ["active", "In progress", (t) => ACTIVE.has(t.s)],
    ["ready", "Ready", (t) => t.s === "ready"],
    ["blocked", "Blocked", (t) => t.s === "blocked"],
    ["done", "Done", (t) => t.s === "done"],
  ];

  pages.tickets = () => {
    const ts = TICKETS.filter(scoped);
    const [, , pred] = TFILTERS.find(([id]) => id === st.tfilter);
    const feats = new Set(ts.map((t) => t.feature)).size, projs = new Set(ts.map((t) => projectOf(t).id)).size;
    main().innerHTML = `
      ${topbar("<strong>All tickets</strong>", scopeChip())}
      <div class="cc-page">
        <div class="cc-page-head">
          <h1>All tickets</h1>
          <p>${ts.filter((t) => t.s !== "done").length} open across ${feats} features in ${projs} project${projs > 1 ? "s" : ""}.</p>
        </div>
        <div class="tabs cc-filter-tabs" role="tablist" aria-label="Filter tickets">
          ${TFILTERS.map(([id, label, p]) => `<button class="tab" role="tab" type="button" aria-selected="${st.tfilter === id}" data-act="tfilter" data-v="${id}">${label}<span class="count">${ts.filter(p).length}</span></button>`).join("")}
        </div>
        ${groupedBoard(ts.filter(pred), { feature: true })}
      </div>`;
  };

  function median(xs) {
    const s = [...xs].sort((a, b) => a - b), m = Math.floor(s.length / 2);
    return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
  }

  function chart(rows) {
    const W = 720, H = 240, L = 44, R = 16, T = 14, B = 30;
    const ymax = Math.max(1, Math.ceil(Math.max(...rows.map((r) => r.total))));
    const x = (i) => L + (rows.length === 1 ? (W - L - R) / 2 : (i * (W - L - R)) / (rows.length - 1));
    const y = (v) => T + (1 - v / ymax) * (H - T - B);
    const med = rows.map((_, i) => median(rows.slice(Math.max(0, i - 9), i + 1).map((r) => r.total)));
    const grid = Array.from({ length: ymax + 1 }, (_, v) => `<line x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}" class="cc-grid${v === 0 ? " is-base" : ""}"/><text x="${L - 8}" y="${y(v)}" dy="0.32em" text-anchor="end" class="cc-tick">${v}%</text>`).join("");
    const xl = [0, rows.length - 1].map((i) => `<text x="${x(i)}" y="${H - 8}" text-anchor="${i ? "end" : "start"}" class="cc-tick">${rows[i].day}</text>`).join("");
    const line = med.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)} ${y(v).toFixed(1)}`).join(" ");
    const dots = rows.map((r, i) => `<circle class="cc-dot" cx="${x(i)}" cy="${y(r.total)}" r="4"/><circle class="cc-hit" cx="${x(i)}" cy="${y(r.total)}" r="14" data-pt="${i}" tabindex="0" role="img" aria-label="${r.title}: ${r.total.toFixed(1)}% of the week"/>`).join("");
    return `<div class="cc-chart" data-chart>
      <svg viewBox="0 0 ${W} ${H}" role="group" aria-label="Spend per merged ticket, ${rows.length} tickets">${grid}${xl}<path class="cc-median" d="${line}"/>${dots}</svg>
      <div class="cc-tip" hidden></div>
    </div>`;
  }

  pages.insights = () => {
    const rows = CC.MERGED.map((m) => ({ ...m, total: m.agent + m.resolve + m.follow }));
    const u = CC.USAGE;
    const pct = (v) => `${v.toFixed(1)}%`;
    main().innerHTML = `
      ${topbar("<strong>Insights</strong>")}
      <div class="cc-page cc-page-narrow">
        <div class="cc-page-head"><h1>Insights</h1><p>What merged work costs, measured against your model limits.</p></div>

        <section class="cc-module" aria-labelledby="lim-h">
          <h2 id="lim-h">Limits</h2>
          <div class="cc-limits">
            <div class="cc-limit"><span>Five-hour window</span><div class="meter"><span style="width:${u.fiveHour.pct}%"></span></div><b>${u.fiveHour.pct}%</b><span class="meta">resets ${u.fiveHour.resets}</span></div>
            <div class="cc-limit"><span>Weekly</span><div class="meter"><span style="width:${u.weekly.pct}%"></span></div><b>${u.weekly.pct}%</b><span class="meta">resets ${u.weekly.resets}</span></div>
          </div>
        </section>

        <section class="cc-module" aria-labelledby="spend-h">
          <div class="cc-module-head"><h2 id="spend-h">Spend per merged ticket</h2>
            <div class="cc-legend"><span><i class="cc-key-dot"></i>Ticket</span><span><i class="cc-key-line"></i>Rolling median of 10</span></div></div>
          <p class="cc-module-sub">Share of the weekly limit each ticket used, from launch to merge.</p>
          ${chart(rows)}
        </section>

        <section class="cc-module" aria-labelledby="tbl-h">
          <h2 id="tbl-h">Merged tickets</h2>
          <div class="cc-table-wrap"><table class="cc-table">
            <thead><tr><th>Ticket</th><th>Feature</th><th>Merged</th><th class="num">Agent</th><th class="num">Resolve</th><th class="num">Follow-up</th><th class="num">Total</th></tr></thead>
            <tbody>${[...rows].reverse().map((r) => `<tr><td>${r.title}</td><td><span class="cc-tfeat">${avatar(project(r.project))}${r.feature}</span></td><td class="meta">${r.day}</td><td class="num">${pct(r.agent)}</td><td class="num">${r.resolve ? pct(r.resolve) : "—"}</td><td class="num">${r.follow ? pct(r.follow) : "—"}</td><td class="num"><b>${pct(r.total)}</b></td></tr>`).join("")}</tbody>
          </table></div>
        </section>
      </div>`;

    const box = main().querySelector("[data-chart]"), tip = box.querySelector(".cc-tip");
    const show = (el) => {
      const r = rows[Number(el.dataset.pt)];
      tip.innerHTML = `<b>${r.title}</b><span>${r.feature} · ${r.day}</span><dl><dt>Agent</dt><dd>${pct(r.agent)}</dd><dt>Resolve</dt><dd>${pct(r.resolve)}</dd><dt>Follow-up</dt><dd>${pct(r.follow)}</dd><dt>Total</dt><dd><b>${pct(r.total)}</b></dd></dl>`;
      const bb = box.getBoundingClientRect(), pb = el.getBoundingClientRect();
      tip.hidden = false;
      const left = Math.min(Math.max(pb.left - bb.left + pb.width / 2 - tip.offsetWidth / 2, 0), bb.width - tip.offsetWidth);
      tip.style.left = `${left}px`;
      tip.style.top = `${pb.top - bb.top - tip.offsetHeight - 6}px`;
      el.previousElementSibling.classList.add("is-on");
    };
    const hide = (el) => { tip.hidden = true; el?.previousElementSibling?.classList.remove("is-on"); };
    box.addEventListener("pointerover", (e) => { if (e.target.dataset.pt) show(e.target); });
    box.addEventListener("pointerout", (e) => { if (e.target.dataset.pt) hide(e.target); });
    box.addEventListener("focusin", (e) => { if (e.target.dataset.pt) show(e.target); });
    box.addEventListener("focusout", (e) => { if (e.target.dataset.pt) hide(e.target); });
  };

  const REPO_GLYPH = { ready: "done", refused: "failed", pending: "running" };
  const REPO_LABEL = { ready: "Ready", refused: "Refused", pending: "Checking" };
  const CHECK_GLYPH = { ok: "done", default: "done", fail: "failed", skip: "blocked", run: "running", wait: "blocked" };
  const checkText = (repo, id) => ({
    ok: "Passed", default: "Missing, defaults in use", fail: repo.reason || "Failed", skip: "Skipped", run: "Running…", wait: "Waiting",
  }[repo.checks[id]]);
  const repoByName = (n) => CC.REPOS.find((r) => r.name === n);
  const repoTickets = (n) => TICKETS.filter((t) => t.repo === n);

  function repoSummary(r) {
    if (r.state === "refused") return `<span class="cc-repo-why">${r.reason}</span>`;
    if (r.state === "pending") return `<span class="shimmer">Checking the repo on GitHub, then cloning it</span>`;
    return r.note ? `<span>${r.note}</span>` : `<span>All four checks passed</span>`;
  }

  function trackResults() {
    const q = (st.trackQ || "").trim().toLowerCase();
    if (!q) return "";
    const hits = CC.GH_SEARCH.filter((n) => n.includes(q)).slice(0, 6);
    if (!hits.length) return `<div class="cc-track-results" role="listbox"><p class="meta">No repos on GitHub match “${st.trackQ}”.</p></div>`;
    return `<div class="cc-track-results" role="listbox">${hits.map((n) => {
      const r = repoByName(n);
      const right = r ? `<span class="badge" data-s="${REPO_GLYPH[r.state]}">${glyph(REPO_GLYPH[r.state])}${REPO_LABEL[r.state]}</span>` : `<button class="btn btn-sm" type="button" data-act="track" data-repo="${n}">Track</button>`;
      return `<div class="cc-track-hit" role="option">${icon("repo", "icon icon-sm")}<span class="mono">${n}</span>${right}</div>`;
    }).join("")}</div>`;
  }

  pages.repos = () => {
    const groups = [...PROJECTS.map((p) => ({ p, repos: CC.REPOS.filter((r) => r.project === p.id) }))].filter((g) => g.repos.length && (st.scope === "all" || g.p.id === st.scope));
    main().innerHTML = `
      ${topbar("<strong>Repos</strong>", scopeChip())}
      <div class="cc-page cc-page-narrow">
        <div class="cc-page-head"><h1>Repos</h1><p>Agents only launch in repos that pass four checks: clone, squash-only merges, a main default branch and a valid settings file.</p></div>
        <form class="cc-track" data-form="track" autocomplete="off">
          <label class="sr-only" for="track-q">Track a repo</label>
          <div class="cc-track-field">${icon("search", "icon icon-sm")}<input id="track-q" class="input" placeholder="Track a repo: search owner/name" value="${st.trackQ || ""}" data-track></div>
          <p class="cc-help">Search runs as this machine's <span class="code">gh</span> identity.</p>
          ${trackResults()}
        </form>
        ${groups.map(({ p, repos }) => `<section class="cc-repo-group">
          <h2 class="nav-label">${avatar(p)}${p.name}<span class="count">${repos.length}</span></h2>
          ${repos.map((r) => {
            const n = repoTickets(r.name).filter((t) => t.s !== "done").length;
            return `<a class="cc-repo-row" href="#/repos/${r.name}">
              ${glyph(REPO_GLYPH[r.state], true)}
              <span class="cc-repo-main"><b class="mono">${r.name}</b>${repoSummary(r)}</span>
              <span class="cc-repo-checks" aria-label="Checks">${CC.CHECKS.map(([id, label]) => `<span title="${label}: ${checkText(r, id)}">${glyph(CHECK_GLYPH[r.checks[id]])}</span>`).join("")}</span>
              <span class="cc-repo-count">${r.state === "ready" ? (n ? `${n} open` : "No open tickets") : REPO_LABEL[r.state]}</span>
              ${icon("chev", "icon icon-sm cc-repo-chev")}
            </a>`;
          }).join("")}
        </section>`).join("")}
      </div>`;
  };

  pages.repo = () => {
    const r = repoByName(st.repo);
    if (!r) {
      main().innerHTML = `${topbar(`<a href="#/repos">Repos</a><span class="sep">/</span><strong>${st.repo}</strong>`)}<div class="cc-page"><div class="cc-empty"><b>${st.repo} isn't tracked</b><span>Track it from the repos page to launch agents in it.</span><a class="btn" href="#/repos">Go to repos</a></div></div>`;
      return;
    }
    const p = project(r.project), ts = repoTickets(r.name);
    let banner = "";
    if (r.state === "refused") banner = `<div class="cc-callout" data-s="failed"><div class="cc-callout-head">${glyph("failed")}Refused · ${r.reason}</div><p>${r.fix}</p>
      <div class="cc-callout-act"><button class="btn btn-primary" type="button" data-act="retrack" data-repo="${r.name}">Track again</button><button class="btn" type="button" data-act="gh-settings" data-repo="${r.name}">Open settings on GitHub</button></div></div>`;
    else if (r.state === "pending") banner = `<div class="cc-callout"><div class="cc-callout-head">${glyph("running", true)}<span class="shimmer">Checking the repo on GitHub, then cloning it</span></div><p>This takes under a minute. Agents can launch here once every check passes.</p></div>`;
    else if (r.note) banner = `<div class="cc-note">${icon("settings", "icon icon-sm")}<span>${r.note} <a class="cc-link" href="#" data-act="toml">See the settings reference</a></span></div>`;

    main().innerHTML = `
      ${topbar(`<a href="#/repos">Repos</a><span class="sep">/</span><strong class="mono">${r.name}</strong>`, r.state === "ready" ? `<a class="btn" href="#/launch">${icon("plus", "icon icon-sm")}<span class="btn-label">Launch</span></a>` : "")}
      <div class="cc-page cc-page-narrow">
        <div class="cc-page-head">
          <span class="badge" data-s="${REPO_GLYPH[r.state]}">${glyph(REPO_GLYPH[r.state])}${REPO_LABEL[r.state]}</span>
          <h1 class="mono cc-repo-title">${r.name}</h1>
          <p>${avatar(p)} ${p.name} · ${ts.filter((t) => t.s !== "done").length} open tickets · base <span class="code">origin/main</span></p>
        </div>
        ${banner}
        <section class="cc-module" aria-labelledby="chk-h">
          <h2 id="chk-h">Checks</h2>
          <ul class="cc-checklist">${CC.CHECKS.map(([id, label]) => `<li>${glyph(CHECK_GLYPH[r.checks[id]], true)}<span>${label}</span><span class="meta">${checkText(r, id)}</span></li>`).join("")}</ul>
        </section>
        <section class="cc-module" aria-labelledby="tk-h">
          <h2 id="tk-h">Tickets</h2>
          ${ts.length ? groupedBoard(ts, { feature: true }) : `<div class="cc-empty"><b>No tickets in this repo yet</b><span>${r.state === "ready" ? "Launch a feature that touches it and its tickets show up here." : "Fix the failing check first."}</span></div>`}
        </section>
      </div>`;
  };

  function runChecks(r, outcome) {
    r.state = "pending";
    CC.CHECKS.forEach(([id]) => { r.checks[id] = "wait"; });
    const ids = CC.CHECKS.map(([id]) => id);
    let i = 0;
    const rerender = () => { if (st.page === "repos" || (st.page === "repo" && st.repo === r.name)) APP.renderMain(); APP.renderRail(); };
    const step = () => {
      if (i > 0) {
        const prev = ids[i - 1];
        const res = outcome[prev] || "ok";
        r.checks[prev] = res;
        if (res === "fail") {
          ids.slice(i).forEach((id) => { r.checks[id] = "skip"; });
          r.state = "refused";
          rerender();
          return toast(`${r.name} is still refused: ${r.reason.toLowerCase()}`);
        }
      }
      if (i === ids.length) {
        r.state = "ready";
        delete r.reason;
        rerender();
        return toast(`${r.name} is ready for agents`);
      }
      r.checks[ids[i]] = "run";
      i++;
      rerender();
      setTimeout(step, 750);
    };
    step();
  }

  function track(name) {
    const owner = name.split("/")[0];
    const proj = name.includes("job-scraper") ? "job-scraper" : owner === "acme" ? "acme" : "command-center";
    const r = { name, project: proj, state: "pending", checks: {}, note: "No .command-centre.toml on main, so defaults are in use." };
    CC.REPOS.push(r);
    st.trackQ = "";
    toast(`Tracking ${name}`);
    runChecks(r, { settings: "default" });
  }

  function launchSets(fid) {
    const ts = ticketsOf(fid);
    return {
      now: ts.filter((t) => t.s === "ready"),
      unlock: ts.filter((t) => t.s === "blocked" && !t.armed),
      armed: ts.filter((t) => t.s === "blocked" && t.armed),
      flight: ts.filter((t) => ACTIVE.has(t.s) || NEEDS_YOU.has(t.s)),
    };
  }

  function launchLabel() {
    const sel = [...st.lsel.keys].map(byKey);
    const now = sel.filter((t) => t.s === "ready").length, arm = sel.length - now;
    if (!sel.length) return "Select tickets to launch";
    return [now && `Launch ${now} now`, arm && `arm ${arm}`].filter(Boolean).join(" · ").replace(/^arm/, "Arm");
  }

  const cand = (t, when) => `<label class="cc-cand">
    <input type="checkbox" data-lpick="${t.key}" ${st.lsel.keys.has(t.key) ? "checked" : ""}>
    ${glyph(t.s)}<span class="mono">${t.ref}</span><span class="cc-cand-t">${t.title}</span>
    <span class="meta">${when === "now" ? t.repo : `after ${t.blockedBy.map((k) => byKey(k).ref).join(", ")}`}</span></label>`;

  pages.launch = () => {
    const scrim = `<div class="cc-scrim" data-close></div>`;
    if (!st.launch) {
      const fs = FEATURES.filter((f) => !f.settled && (st.scope === "all" || f.project === st.scope));
      return `${scrim}<div class="cc-dialog" role="dialog" aria-modal="true" aria-labelledby="dlg-t">
        <div class="cc-dialog-head"><h2 id="dlg-t">Launch a feature</h2><button class="btn btn-ghost btn-sm btn-icon" type="button" aria-label="Close" data-close>${icon("x", "icon icon-sm")}</button></div>
        <div class="cc-dialog-body"><div class="cc-pick-list">${fs.map((f) => {
          const s = launchSets(f.id), p = project(f.project);
          return `<a class="cc-pick" href="#/f/${f.id}/launch">${avatar(p)}<span><b>${f.title}</b><span class="meta">${p.name} · ${featureStats(f.id).total} tickets</span></span>
            <span class="cc-pick-n">${s.now.length ? `${s.now.length} ready` : ""}${s.now.length && s.unlock.length ? " · " : ""}${s.unlock.length ? `${s.unlock.length} on unlock` : ""}${!s.now.length && !s.unlock.length ? "Nothing to launch" : ""}</span>${icon("chev", "icon icon-sm")}</a>`;
        }).join("")}</div></div>
      </div>`;
    }
    const f = feature(st.launch), p = project(f.project), s = launchSets(f.id);
    if (!st.lsel || st.lsel.fid !== f.id) st.lsel = { fid: f.id, keys: new Set([...s.now, ...s.unlock].map((t) => t.key)) };
    const empty = !s.now.length && !s.unlock.length;
    return `${scrim}<form class="cc-dialog cc-dialog-launch" role="dialog" aria-modal="true" aria-labelledby="dlg-t" data-form="launch">
      <div class="cc-dialog-head"><span class="cc-dialog-proj">${avatar(p)}${p.name}</span><h2 id="dlg-t">Launch ${f.title}</h2>
        <a class="btn btn-ghost btn-sm" href="#/launch">Change feature</a><button class="btn btn-ghost btn-sm btn-icon" type="button" aria-label="Close" data-close>${icon("x", "icon icon-sm")}</button></div>
      <div class="cc-dialog-body">
        ${empty ? `<div class="cc-empty"><b>Nothing to launch</b><span>Every ticket in ${f.title} is running, waiting on you, merged or already armed.</span></div>` : `
        ${s.now.length ? `<fieldset class="cc-cands"><legend>Ready now <span class="meta">no blockers, launch in fresh worktrees</span></legend>${s.now.map((t) => cand(t, "now")).join("")}</fieldset>` : ""}
        ${s.unlock.length ? `<fieldset class="cc-cands"><legend>On unlock <span class="meta">armed, each launches the moment its blockers merge</span></legend>${s.unlock.map((t) => cand(t, "unlock")).join("")}</fieldset>` : ""}`}
        ${s.flight.length || s.armed.length ? `<details class="cc-flight"><summary>${s.flight.length + s.armed.length} already in flight or armed</summary>
          <ul>${[...s.flight, ...s.armed].map((t) => `<li>${glyph(t.s)}<span class="mono">${t.ref}</span>${t.title}<span class="meta">${t.armed ? "Armed" : CC.LABEL[t.s]}</span></li>`).join("")}</ul></details>` : ""}
      </div>
      ${empty ? "" : `<div class="cc-dialog-foot">
        <div class="composer">
          <label class="sr-only" for="lc">Extra context for every agent</label>
          <textarea id="lc" rows="2" placeholder="Extra context for every agent in this launch (optional)"></textarea>
          <div class="composer-bar">
            <button class="picker" type="button">${icon("bolt", "icon icon-sm")}Sonnet 5.5${icon("down", "icon icon-sm")}</button><span class="sep"></span>
            <button class="picker" type="button">Medium${icon("down", "icon icon-sm")}</button><span class="sep"></span>
            <button class="picker" type="button">Base origin/main${icon("down", "icon icon-sm")}</button>
          </div>
        </div>
        <div class="cc-dialog-actions"><button class="btn btn-ghost" type="button" data-close>Cancel</button><button class="btn btn-primary" type="submit" data-launch-btn ${st.lsel.keys.size ? "" : "disabled"}>${launchLabel()} <kbd>⌘↵</kbd></button></div>
      </div>`}
    </form>`;
  };

  const KEYS = [
    ["Anywhere", [["⌘K", "Search sessions"], ["⌘0–3", "Switch project scope"], ["C", "Launch a feature"], ["G then H / T / I / R", "Go to home, tickets, insights, repos"], ["J / K", "Next / previous session"], ["?", "This sheet"], ["Esc", "Close"]]],
    ["In a session", [["F", "Open the feature graph"], ["E", "Settle"], ["R", "Re-run"], ["O", "Open the PR"], ["Enter", "Message the agent"]]],
    ["In a feature", [["V", "Switch graph and board"]]],
  ];
  pages.keys = () => `<div class="cc-scrim" data-close></div><div class="cc-dialog cc-dialog-sm" role="dialog" aria-modal="true" aria-labelledby="keys-t">
    <div class="cc-dialog-head"><h2 id="keys-t">Keyboard shortcuts</h2><button class="btn btn-ghost btn-sm btn-icon" type="button" aria-label="Close" data-close autofocus>${icon("x", "icon icon-sm")}</button></div>
    <div class="cc-dialog-body">${KEYS.map(([h, ks]) => `<h3 class="cc-keys-h">${h}</h3><dl class="cc-keys">${ks.map(([k, d]) => `<dt>${k.split(" ").map((x) => (/^(then|\/)$/.test(x) ? `<span class="meta">${x}</span>` : `<kbd>${x}</kbd>`)).join(" ")}</dt><dd>${d}</dd>`).join("")}</dl>`).join("")}</div>
  </div>`;

  pages.onAct = (a, el) => {
    if (a === "tfilter") { st.tfilter = el.dataset.v; return APP.renderMain(); }
    if (a === "track") return track(el.dataset.repo);
    if (a === "retrack") { const r = repoByName(el.dataset.repo); const failOn = Object.keys(r.checks).find((k) => r.checks[k] === "fail"); return runChecks(r, { [failOn]: "fail", settings: r.checks.settings === "default" ? "default" : "ok" }); }
    if (a === "gh-settings") return toast(`Opens github.com/${el.dataset.repo}/settings`);
    if (a === "toml") return toast("Opens the .command-centre.toml reference");
  };

  pages.onInput = (e) => {
    if (e.target.matches("[data-track]")) {
      st.trackQ = e.target.value;
      const form = e.target.closest("form");
      form.querySelector(".cc-track-results")?.remove();
      form.insertAdjacentHTML("beforeend", trackResults());
    }
  };

  pages.onChange = (e) => {
    if (e.target.matches("[data-lpick]")) {
      const k = e.target.dataset.lpick;
      e.target.checked ? st.lsel.keys.add(k) : st.lsel.keys.delete(k);
      const b = document.querySelector("[data-launch-btn]");
      b.disabled = !st.lsel.keys.size;
      b.innerHTML = `${launchLabel()} <kbd>⌘↵</kbd>`;
    }
  };

  pages.onSubmit = (e) => {
    const form = e.target.dataset.form;
    if (form === "track") {
      const q = (st.trackQ || "").trim();
      const hit = CC.GH_SEARCH.find((n) => n === q) || CC.GH_SEARCH.find((n) => n.includes(q));
      if (hit && !repoByName(hit)) track(hit); else if (hit) location.hash = `#/repos/${hit}`;
      return true;
    }
    if (form === "launch") {
      const sel = [...st.lsel.keys].map(byKey);
      if (!sel.length) return true;
      const prev = sel.map((t) => ({ t, s: t.s, armed: t.armed, elapsed: t.elapsed, last: t.last, reason: t.reason, verbs: t.verbs, turns: t.turns, tokens: t.tokens }));
      sel.forEach((t) => {
        if (t.s === "ready") Object.assign(t, { s: "running", reason: "Agent running", elapsed: 0, last: "Starting in a fresh worktree from origin/main", verbs: ["Kill"], updated: 0 });
        else t.armed = true;
      });
      const label = launchLabel();
      st.lsel = null;
      APP.go(`#/f/${st.launch}/graph`);
      toast(label.replace(/^Launch/, "Launched").replace("arm", "armed").replace(/^Arm/, "Armed"), () => { prev.forEach(({ t, ...o }) => Object.assign(t, o)); APP.render(); });
      return true;
    }
    return false;
  };

  document.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      const f = document.querySelector('[data-form="launch"]');
      if (f) { e.preventDefault(); f.requestSubmit(); }
    }
  });
})();
