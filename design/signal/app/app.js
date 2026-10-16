window.APP = (() => {
  const { TICKETS, FEATURES, PROJECTS, NEEDS_YOU, ACTIVE, LABEL, byKey, feature, project, projectOf, ticketsOf, depth, featureStats, unlocks, icon, glyph, avatar, ago, fmtDur, toast } = CC;

  const firstNeeds = () => TICKETS.filter((t) => NEEDS_YOU.has(t.s)).sort((a, b) => a.updated - b.updated)[0];
  const st = {
    scope: "all", q: "", sel: firstNeeds().key, settledOpen: false, menu: false,
    open: new Set(FEATURES.filter((f) => !f.settled).map((f) => f.id)),
    blockedOpen: new Set(), settledKeys: new Set(),
    page: "session", fid: null, ftab: "graph", logMode: "transcript", launch: null, tfilter: "all", repo: null,
    lastHash: "#/",
  };
  const pages = {};

  const inScope = (t) => (st.scope === "all" || projectOf(t).id === st.scope)
    && (!st.q || `${t.title} ${t.ref} ${t.repo} ${feature(t.feature).title}`.toLowerCase().includes(st.q.toLowerCase()));
  const isSettled = (t) => t.s === "done" || feature(t.feature).settled || st.settledKeys.has(t.key);
  const slug = (t) => `cc-${t.ref.slice(1)}-${t.title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  const sessionHash = (key) => `#/s/${encodeURIComponent(key)}`;
  let here = location.hash || "#/";
  const go = (hash) => {
    here = hash;
    try { if (location.hash !== hash) history.pushState(null, "", hash); } catch { /* sandboxed preview frames can refuse history writes */ }
    route();
  };

  let app, rail, main, overlay;

  function card(t) {
    const p = projectOf(t), f = feature(t.feature);
    const verbs = (t.verbs || []).slice(0, 2).map((v, i) => `<button class="btn btn-sm${i === 0 && t.s === "attention" ? " btn-primary" : ""}" type="button" data-verb="${v}">${v}</button>`).join("");
    return `<div class="cc-card${st.page === "session" && st.sel === t.key ? " is-sel" : ""}" role="button" tabindex="0" data-key="${t.key}">
      <div class="cc-card-top">${glyph(t.s)}<span class="cc-card-title">${t.title}</span><span class="meta">${ago(t.updated)}</span></div>
      <div class="cc-card-sub">${avatar(p)}${p.name}<span class="meta">·</span>${f.title}<span class="mono">${t.ref}</span></div>
      <p class="cc-card-last"><b data-s="${t.s}">${t.reason}.</b> ${t.last}</p>
      <div class="cc-card-act">${verbs}</div>
    </div>`;
  }

  function row(t) {
    const metric = t.s === "running" ? `<span class="mono" data-elapsed="${t.key}">${fmtDur(t.elapsed)}</span>`
      : t.armed ? `<span class="cc-armed">Armed</span>`
      : t.checks ? `<span class="mono">${t.checks[0]}/${t.checks[1]}</span>` : "";
    return `<div class="cc-row${st.page === "session" && st.sel === t.key ? " is-sel" : ""}${t.s === "blocked" ? " is-blocked" : ""}" role="button" tabindex="0" data-key="${t.key}">
      ${glyph(t.s, true)}<span class="t">${t.title}</span>${metric}</div>`;
  }

  const navItem = (hash, ic, label, count, current) =>
    `<a class="nav-item" href="${hash}" ${current ? 'aria-current="page"' : ""}>${icon(ic)}${label}${count != null ? `<span class="count">${count}</span>` : ""}</a>`;

  function renderRail() {
    const live = TICKETS.filter((t) => inScope(t) && !isSettled(t));
    const needs = live.filter((t) => NEEDS_YOU.has(t.s)).sort((a, b) => CC.RANK[a.s] - CC.RANK[b.s] || a.updated - b.updated);
    const groups = FEATURES.filter((f) => !f.settled).map((f) => {
      const ts = live.filter((t) => t.feature === f.id && !NEEDS_YOU.has(t.s));
      return { f, active: ts.filter((t) => ACTIVE.has(t.s) || t.s === "ready"), blocked: ts.filter((t) => t.s === "blocked") };
    }).filter((g) => g.active.length || g.blocked.length);
    const settled = TICKETS.filter((t) => inScope(t) && isSettled(t));
    const scopeP = project(st.scope);
    const running = TICKETS.filter((t) => t.s === "running").length;
    const refused = CC.REPOS.filter((r) => r.state === "refused").length;
    const openTickets = TICKETS.filter((t) => t.s !== "done" && (st.scope === "all" || projectOf(t).id === st.scope)).length;

    rail.innerHTML = `
      <div class="cc-rail-head">
        <a class="cc-brand" href="#/"><span class="cc-mark">${icon("term")}</span><b>Command Centre</b></a>
        <button class="btn btn-ghost btn-sm btn-icon" type="button" aria-label="Launch a feature (C)" title="Launch a feature  C" data-act="new">${icon("plus", "icon icon-sm")}</button>
      </div>
      <div class="cc-tools">
        <label class="cc-search">${icon("search", "icon icon-sm")}<span class="sr-only">Search sessions</span>
          <input type="search" placeholder="Search or jump to…" value="${st.q}" data-search autocomplete="off"><kbd>⌘K</kbd></label>
        <div class="cc-scope">
          <button class="cc-scope-btn" type="button" aria-haspopup="menu" aria-expanded="${st.menu}" data-act="scope">
            ${scopeP ? avatar(scopeP) + scopeP.name : icon("layers", "icon icon-sm") + "All projects"}
            <span class="meta" style="font-weight:400">${running} running</span>${icon("down")}
          </button>
          <div class="cc-menu" role="menu" ${st.menu ? "" : "hidden"}>
            <button role="menuitemradio" aria-checked="${st.scope === "all"}" data-scope="all">${icon("layers", "icon icon-sm")}All projects<kbd>⌘0</kbd></button>
            ${PROJECTS.map((p, i) => `<button role="menuitemradio" aria-checked="${st.scope === p.id}" data-scope="${p.id}">${avatar(p)}${p.name}${i < 9 ? `<kbd>⌘${i + 1}</kbd>` : ""}</button>`).join("")}
          </div>
        </div>
        <nav class="cc-nav" aria-label="Destinations">
          ${navItem("#/tickets", "filter", "All tickets", openTickets, st.page === "tickets")}
          ${navItem("#/insights", "chart", "Insights", null, st.page === "insights")}
          <a class="nav-item" href="#/repos" ${st.page === "repos" || st.page === "repo" ? 'aria-current="page"' : ""}>${icon("repo")}Repos${refused ? `<span class="count cc-count-warn">${refused} refused</span>` : ""}</a>
        </nav>
      </div>
      <div class="cc-rail-scroll">
        ${needs.length ? `<section class="cc-sec"><h3 class="nav-label">Needs you <span class="count">${needs.length}</span></h3>${needs.map(card).join("")}</section>` : ""}
        <section class="cc-sec"><h3 class="nav-label">In flight</h3>
          ${groups.map(({ f, active, blocked }) => {
            const s = featureStats(f.id), p = project(f.project), open = st.open.has(f.id), bOpen = st.blockedOpen.has(f.id);
            return `<div class="cc-group">
              <div class="cc-ghead${st.page === "feature" && st.fid === f.id ? " is-current" : ""}">
                <button class="cc-gtoggle" type="button" aria-expanded="${open}" aria-label="${open ? "Collapse" : "Expand"} ${f.title}" data-toggle="${f.id}">${icon("chev")}</button>
                <a class="cc-gopen" href="#/f/${f.id}/${st.ftab}" title="Open ${f.title}">${avatar(p)}<span>${f.title}</span>${icon("graph", "icon cc-gicon")}<span class="cc-prog">${s.merged}/${s.total}</span></a>
              </div>
              <div class="cc-rows" ${open ? "" : "hidden"}>
                ${active.map(row).join("")}
                ${blocked.length ? (bOpen ? blocked.map(row).join("") + `<button class="cc-more" type="button" data-blocked="${f.id}">Hide blocked</button>`
                  : `<button class="cc-more" type="button" data-blocked="${f.id}"><span class="status" data-s="blocked"></span>${blocked.length} blocked, waiting on this wave</button>`) : ""}
              </div>
            </div>`;
          }).join("") || `<p class="meta cc-empty-note">${st.q ? `Nothing matches “${st.q}”.` : "Nothing running in this scope."}</p>`}
        </section>
        <section class="cc-sec">
          <button class="nav-label cc-settled-toggle" type="button" aria-expanded="${st.settledOpen}" data-act="settled">Settled <span class="count">${settled.length}</span>${icon("down")}</button>
          <div class="cc-rows" ${st.settledOpen ? "" : "hidden"}>${settled.map(row).join("")}</div>
        </section>
      </div>
      <footer class="cc-rail-foot">
        <a class="cc-usage" href="#/insights" title="Usage limits">
          <span>5h</span><div class="meter"><span style="width:${CC.USAGE.fiveHour.pct}%"></span></div><span class="meta">${CC.USAGE.fiveHour.pct}%</span>
          <span>Week</span><div class="meter"><span style="width:${CC.USAGE.weekly.pct}%"></span></div><span class="meta">${CC.USAGE.weekly.pct}%</span>
        </a>
        <button class="btn btn-ghost btn-sm cc-keys-btn" type="button" data-act="keys">${icon("bolt", "icon icon-sm")}Shortcuts<kbd>?</kbd></button>
      </footer>`;
  }

  function topbar(crumbs, end = "") {
    return `<header class="cc-top">
      <a class="btn btn-ghost btn-icon cc-back" href="#/" aria-label="Back to sessions">${icon("back")}</a>
      <nav class="cc-crumbs" aria-label="Breadcrumb">${crumbs}</nav>
      <div class="cc-top-end">${end}</div>
    </header>`;
  }

  function stack(t) {
    const ts = ticketsOf(t.feature);
    const waves = Math.max(...ts.map((x) => depth(x))) + 1;
    return Array.from({ length: waves }, (_, w) => {
      const items = ts.filter((x) => depth(x) === w).sort((a, b) => CC.RANK[a.s] - CC.RANK[b.s]);
      return `<div class="cc-wave"><div class="cc-wave-label">Wave ${w + 1}</div><div class="cc-rows">${items.map(row).join("")}</div></div>`;
    }).join("");
  }

  function stateBlock(t) {
    const un = unlocks(t);
    const unlockLine = un.length ? `<div class="cc-unlocks">Merging this unlocks ${un.map((u) => `<a class="chip chip-mono" href="${sessionHash(u.key)}">${glyph(u.s)}${u.ref}</a>`).join("")}</div>` : "";
    if (t.s === "failed") {
      return `<div class="cc-callout" data-s="failed">
        <div class="cc-callout-head">${glyph("failed")}${t.reason}<span class="mono">${t.pr ? `PR ${t.pr}` : ""}${t.checks ? ` · ${t.checks[0]}/${t.checks[1]} checks` : ""}</span></div>
        <p>${t.detail}.</p>
        <div class="cc-callout-act">${(t.verbs || []).map((v, i) => `<button class="btn${i === 0 ? " btn-primary" : ""}" type="button" data-verb="${v}">${v}${i === 0 ? " <kbd>R</kbd>" : ""}</button>`).join("")}</div>
        ${unlockLine}
      </div>`;
    }
    if (t.s === "attention") {
      return `<div class="cc-callout" data-s="attention">
        <div class="cc-callout-head">${glyph("attention")}Pull request ${t.pr} is ready<span class="mono">+${t.diff[0]} −${t.diff[1]} · ${t.checks[0]}/${t.checks[1]} checks</span></div>
        <p>${t.detail}. Review and merge on GitHub; Command Centre picks up the merge.</p>
        <div class="cc-callout-act"><button class="btn btn-primary" type="button" data-verb="Open PR">Open PR ${t.pr} <kbd>O</kbd></button><button class="btn" type="button" data-act="settle">Settle <kbd>E</kbd></button></div>
        ${unlockLine}
      </div>`;
    }
    return unlockLine;
  }

  const fileList = (files) => files.map(([p, a, d]) => `<li><span class="mono">${p}</span><span class="cc-diff"><span class="add">+${a}</span><span class="del">−${d}</span></span></li>`).join("");

  function entry(e) {
    const at = `<span class="cc-at">${e.at || ""}</span>`;
    if (e.t === "phase") return `<div class="cc-phase">${e.text}</div>`;
    if (e.t === "msg") return `<div class="cc-log cc-log-msg">${at}<span class="cc-ico"></span><p class="cc-msg">${e.text}</p></div>`;
    if (e.t === "you") return `<div class="cc-log cc-log-you">${at}<span class="cc-ico"></span><div class="bubble">${e.text}<span class="meta">You, mid-run</span></div></div>`;
    if (e.t === "live") return `<div class="cc-log">${at}<span class="cc-ico">${glyph("running", true)}</span><span class="shimmer">${e.text}</span></div>`;
    if (e.t === "sys") return `<div class="cc-log cc-log-sys">${at}<span class="cc-ico">${icon("term", "icon icon-sm")}</span><span>${e.text}${e.code ? ` <span class="code">${e.code}</span>` : ""}${e.tail ? ` <span class="meta">${e.tail}</span>` : ""}</span></div>`;
    if (e.t === "git") return `<div class="cc-log cc-log-sys">${at}<span class="cc-ico">${icon("merged", "icon icon-sm")}</span><span>${e.text} <span class="code">${e.code}</span></span></div>`;
    if (e.t === "pr") return `<div class="cc-log">${at}<span class="cc-ico">${icon("review", "icon icon-sm")}</span><span><b>${e.text}</b> <span class="meta">${e.tail}</span></span></div>`;
    if (e.t === "tools") return `<div class="cc-log">${at}<span class="cc-ico">${icon("search", "icon icon-sm")}</span><div><button class="disclosure" type="button" aria-expanded="false" data-disclose>${e.text}${icon("chev", "icon icon-sm")}</button>
      <ul class="cc-sub" hidden>${e.items.map(([k, v]) => `<li><span class="cc-kind">${k}</span><span class="mono">${v}</span></li>`).join("")}</ul></div></div>`;
    if (e.t === "edit") {
      const a = e.files.reduce((n, f) => n + f[1], 0), d = e.files.reduce((n, f) => n + f[2], 0);
      return `<div class="cc-log">${at}<span class="cc-ico">${icon("layers", "icon icon-sm")}</span><div><button class="disclosure" type="button" aria-expanded="false" data-disclose>Edited ${e.files.length} file${e.files.length > 1 ? "s" : ""} <span class="cc-diff"><span class="add">+${a}</span><span class="del">−${d}</span></span>${icon("chev", "icon icon-sm")}</button>
        <ul class="cc-sub cc-files" hidden>${fileList(e.files)}</ul></div></div>`;
    }
    if (e.t === "cmd") {
      const ok = e.exit === 0;
      return `<div class="cc-log">${at}<span class="cc-ico">${icon("term", "icon icon-sm")}</span><div class="cc-cmd${ok ? "" : " is-fail"}">
        <button class="cc-cmd-head" type="button" aria-expanded="${!!e.open}" data-disclose><span class="mono">$ ${e.cmd}</span><span class="cc-exit">${ok ? `${icon("check", "icon icon-sm")}${e.dur}` : `exit ${e.exit} · ${e.dur}`}</span></button>
        <pre ${e.open ? "" : "hidden"}>${e.out.join("\n")}</pre></div></div>`;
    }
    if (e.t === "checks") {
      const pass = e.items.filter((c) => c[1] === "pass").length;
      return `<div class="cc-log">${at}<span class="cc-ico">${glyph(pass === e.items.length ? "done" : "failed")}</span><div><b class="cc-checks-title">Checks ${pass}/${e.items.length} passing</b>
        <ul class="cc-checks">${e.items.map(([n, s, d]) => `<li>${glyph(s === "pass" ? "done" : "failed")}<span>${n}</span><span class="meta">${d}</span></li>`).join("")}</ul></div></div>`;
    }
    if (e.t === "error") return `<div class="cc-log">${at}<span class="cc-ico"></span><div class="cc-err"><b>${e.title}</b><pre>${e.out.join("\n")}</pre></div></div>`;
    return "";
  }

  const transcript = (t) => `<div class="cc-log-list">${CC.logFor(t).map(entry).join("")}</div>`;

  function rawLog(t) {
    const strip = (s) => String(s).replace(/<[^>]+>/g, "");
    const lines = CC.logFor(t).flatMap((e) => {
      const head = (kind, text) => `<span class="cc-at">${e.at || "live"}</span><span class="cc-kind">${kind}</span>${text}`;
      const cont = (o, cls = "meta") => `<span class="cc-at"></span><span class="cc-kind"></span><span class="${cls}">${o}</span>`;
      if (e.t === "phase") return [head("phase", e.text)];
      if (e.t === "tools") return e.items.map(([k, v]) => head(k, v));
      if (e.t === "edit") return e.files.map(([p, a, d]) => head("edit", `${p}  +${a} −${d}`));
      if (e.t === "cmd") return [head("bash", `${e.cmd}  → exit ${e.exit} (${e.dur})`), ...e.out.map((o) => cont(o))];
      if (e.t === "checks") return e.items.map(([n, s, d]) => head("check", `${n}  ${s} (${d})`));
      if (e.t === "error") return [head("error", e.title), ...e.out.map((o) => cont(o, "cc-raw-err"))];
      return [head(e.t === "msg" ? "agent" : e.t, strip(`${e.text || ""}${e.code ? " " + e.code : ""}${e.tail ? " " + e.tail : ""}`))];
    });
    return `<div class="cc-raw"><div class="cc-raw-head"><span class="mono">~/.cc/runs/${t.repo.split("/")[1]}/${t.ref.slice(1)}.jsonl</span><span class="meta">${lines.length} lines</span></div>${lines.map((l) => `<div class="cc-raw-line">${l}</div>`).join("")}</div>`;
  }

  function renderSession() {
    const t = byKey(st.sel);
    const p = projectOf(t), f = feature(t.feature);
    const topVerbs = t.s === "running" ? `<button class="btn btn-danger" type="button" data-verb="Kill">${icon("x", "icon icon-sm")}<span class="btn-label">Kill</span></button>`
      : t.pr ? `<button class="btn" type="button" data-verb="Open PR">${icon("review", "icon icon-sm")}<span class="btn-label">PR ${t.pr}</span></button>` : "";
    let body;
    if (t.s === "blocked" || t.s === "ready") {
      const blocked = t.s === "blocked";
      body = `<div class="cc-head"><span class="badge" data-s="${t.s}">${glyph(t.s)}${blocked ? `Blocked · ${t.detail}` : "Ready to launch"}</span><h1>${t.title}</h1>
          <p>${t.repo} ${t.ref} · ${blocked ? (t.armed ? "armed: launches the moment every blocker merges." : "not armed yet.") : "no blockers, nothing running."}</p></div>
        ${blocked ? `<ul class="cc-waiting">${t.blockedBy.map((k) => { const b = byKey(k); return `<li><a href="${sessionHash(b.key)}">${glyph(b.s)}<span class="mono">${b.ref}</span>${b.title}<span class="meta">${LABEL[b.s]}</span></a></li>`; }).join("")}</ul>
          <label class="cc-switch"><input type="checkbox" ${t.armed ? "checked" : ""} data-arm> Launch the moment it unblocks</label>`
          : `<div class="cc-callout-act"><a class="btn btn-primary" href="#/f/${f.id}/launch">Launch from ${f.title}</a></div>`}`;
    } else {
      const running = t.s === "running";
      body = `<div class="cc-head"><span class="badge" data-s="${t.s}">${glyph(t.s)}${LABEL[t.s]}</span><h1>${t.title}</h1><p>${t.repo} ${t.ref} · <span class="mono">${slug(t)}</span></p></div>
        <div class="cc-prompt"><span class="meta">Launched from the ticket · ${f.launched}</span><div class="bubble">Implement ${t.repo}${t.ref}, “${t.title}”. Stay inside the ticket's scope, keep existing tests green, and open a PR against main.</div></div>
        <div class="cc-worked">${running ? `<span class="shimmer">Working for <span data-elapsed="${t.key}">${fmtDur(t.elapsed)}</span></span>` : `Worked · ${t.spend || "$0.00"}`}${t.turns ? `<span class="meta">· ${t.turns} turns · ${t.tokens.toLocaleString()} tokens</span>` : ""}
          <div class="tabs cc-logtabs" role="tablist" aria-label="Log view"><button class="tab" role="tab" aria-selected="${st.logMode === "transcript"}" data-logmode="transcript">Transcript</button><button class="tab" role="tab" aria-selected="${st.logMode === "raw"}" data-logmode="raw">Raw log</button></div></div>
        ${st.logMode === "raw" ? rawLog(t) : transcript(t)}
        ${stateBlock(t)}`;
    }
    const placeholder = { running: "Steer this agent while it works…", failed: "Tell the agent how to fix it, or re-run as is…", attention: "Ask for changes before you review…", blocked: "Add context for when this launches…", ready: "Add context for when this launches…" }[t.s] || "Ask about this run…";

    main.innerHTML = `
      ${topbar(`<a class="cc-crumb-proj" href="#/" data-scope-link="${p.id}">${avatar(p)} ${p.name}</a><span class="sep cc-crumb-sep1">/</span><a class="cc-crumb-link" href="#/f/${f.id}/${st.ftab}">${f.title}</a><span class="sep">/</span><strong>${t.title}</strong>`,
        `${topVerbs}<button class="btn btn-ghost btn-icon" type="button" aria-label="Toggle stack panel" data-act="panel">${icon("sidebar")}</button>`)}
      <div class="cc-body">
        <div class="cc-thread-wrap">
          <div class="cc-thread"><div class="cc-thread-inner">${body}</div></div>
          ${t.s === "done" ? "" : `<div class="cc-composer-wrap"><form class="composer" data-composer>
            <label class="sr-only" for="cc-c">Message the agent</label>
            <textarea id="cc-c" rows="2" placeholder="${placeholder}"></textarea>
            <div class="composer-bar">
              <button class="picker" type="button">${icon("bolt", "icon icon-sm")}Sonnet 5.5${icon("down", "icon icon-sm")}</button><span class="sep"></span>
              <button class="picker" type="button">Medium${icon("down", "icon icon-sm")}</button><span class="sep"></span>
              <button class="picker" type="button">${icon("lock", "icon icon-sm")}Worktree${icon("down", "icon icon-sm")}</button>
              <button class="send" type="submit" aria-label="Send">${icon("send")}</button>
            </div>
          </form></div>`}
        </div>
        <aside class="cc-panel" aria-label="Feature stack">
          <h2>${avatar(p)}${f.title}<span class="meta">${featureStats(f.id).merged}/${featureStats(f.id).total} merged</span></h2>
          <div class="cc-panel-links"><a class="btn btn-sm" href="#/f/${f.id}/graph">${icon("graph", "icon icon-sm")}Graph</a><a class="btn btn-sm" href="#/f/${f.id}/board">${icon("filter", "icon icon-sm")}Board</a></div>
          ${stack(t)}
          <dl class="cc-props">
            <dt>Repo</dt><dd><a class="cc-link" href="#/repos/${t.repo}">${t.repo}</a></dd>
            <dt>Branch</dt><dd class="mono">${slug(t)}</dd>
            <dt>PR</dt><dd>${t.pr || "—"}</dd>
            <dt>Checks</dt><dd>${t.checks ? `${t.checks[0]}/${t.checks[1]} passing` : "—"}</dd>
            <dt>Spend</dt><dd class="mono">${t.spend || "—"}</dd>
          </dl>
        </aside>
      </div>`;
  }

  function gnode(t) {
    const tag = NEEDS_YOU.has(t.s) ? `<span class="tag" data-s="${t.s}">${t.s === "failed" ? t.reason : "Review"}</span>`
      : t.s === "running" ? `<span class="tag mono" data-elapsed="${t.key}">${fmtDur(t.elapsed)}</span>`
      : t.armed ? `<span class="tag">Armed</span>` : "";
    const foot = t.s === "running" ? `<span class="shimmer">${t.last}</span>`
      : t.s === "blocked" ? `after ${t.blockedBy.map((k) => byKey(k).ref).join(", ")}`
      : t.s === "done" ? `Merged ${t.pr}` : t.s === "ready" ? "Ready to launch"
      : `${t.pr ? `PR ${t.pr}` : LABEL[t.s]}${t.checks ? ` · ${t.checks[0]}/${t.checks[1]} checks` : ""}`;
    return `<a class="cc-node" href="${sessionHash(t.key)}" data-node="${t.key}" data-s="${t.s}">
      <span class="cc-node-top">${glyph(t.s, true)}<span class="mono">${t.ref}</span>${tag}</span>
      <span class="cc-node-title">${t.title}</span><span class="cc-node-foot">${foot}</span></a>`;
  }

  function boardRow(t, opts = {}) {
    const metric = t.s === "running" ? `<span class="mono" data-elapsed="${t.key}">${fmtDur(t.elapsed)}</span>` : t.armed ? `<span class="cc-armed">Armed</span>` : `<span class="mono">${t.spend || "—"}</span>`;
    const reason = t.s === "blocked" ? `Blocked by ${t.blockedBy.map((k) => byKey(k).ref).join(", ")} · ${t.detail}` : t.s === "running" ? `<span class="shimmer">${t.last}</span>` : (t.detail || t.reason);
    const where = opts.feature ? (() => { const f = feature(t.feature); return `<span class="chip cc-fchip">${avatar(project(f.project))}${f.title}</span>`; })() : t.repo;
    return `<a class="cc-brow" href="${sessionHash(t.key)}">${glyph(t.s, true)}<span class="mono cc-bref">${t.ref}</span>
      <span class="cc-btitle"><b>${t.title}</b><span>${reason}</span></span>
      <span class="cc-bcell">${where}</span><span class="cc-bcell mono">${t.checks ? `${t.checks[0]}/${t.checks[1]}` : "—"}</span><span class="cc-bcell mono">${t.pr || "—"}</span><span class="cc-bcell cc-bm">${metric}</span></a>`;
  }

  const GROUPS = [
    ["Needs you", (t) => NEEDS_YOU.has(t.s)],
    ["In progress", (t) => ACTIVE.has(t.s)],
    ["Ready to launch", (t) => t.s === "ready"],
    ["Blocked", (t) => t.s === "blocked"],
    ["Done", (t) => t.s === "done"],
  ];

  function groupedBoard(ts, opts) {
    const html = GROUPS.map(([label, pred]) => {
      const items = ts.filter(pred).sort((a, b) => CC.RANK[a.s] - CC.RANK[b.s] || a.updated - b.updated);
      return items.length ? `<div class="cc-bgroup">${label}<span class="meta">${items.length}</span></div>${items.map((t) => boardRow(t, opts)).join("")}` : "";
    }).join("");
    return `<div class="cc-board">${html || `<div class="cc-empty">${glyph("done")}<b>Nothing here</b><span>No tickets match this view.</span></div>`}</div>`;
  }

  const launchable = (fid) => ticketsOf(fid).filter((t) => t.s === "ready" || (t.s === "blocked" && !t.armed));

  function seg(s) {
    return [[s.merged, "--s-done"], [s.needs, "--accent"], [s.active, "--s-running"], [s.blocked + s.ready, "--border-strong"]]
      .filter(([n]) => n).map(([n, c]) => `<span style="flex:${n};background:var(${c})"></span>`).join("");
  }

  function renderFeature() {
    const f = feature(st.fid), p = project(f.project), ts = ticketsOf(f.id), s = featureStats(f.id);
    const waves = Math.max(...ts.map((t) => depth(t))) + 1;
    const nLaunch = launchable(f.id).length;
    const graph = `<div class="cc-graph-scroll"><div class="cc-graph"><svg class="cc-wires" aria-hidden="true"></svg>${Array.from({ length: waves }, (_, w) => {
      const items = ts.filter((t) => depth(t) === w).sort((a, b) => CC.RANK[a.s] - CC.RANK[b.s]);
      const live = items.filter((t) => ACTIVE.has(t.s)).length;
      return `<div class="cc-gwave"><div class="cc-gwave-label"><b>Wave ${w + 1}</b>${w && items.every((t) => t.s === "blocked") ? `unlocks as wave ${w} merges` : `${live} in progress`}</div>${items.map(gnode).join("")}</div>`;
    }).join("")}</div></div>`;
    const tabs = (cls) => `<div class="tabs ${cls}" role="tablist" aria-label="Feature view">
      <a class="tab" role="tab" aria-selected="${st.ftab === "graph"}" href="#/f/${f.id}/graph">${icon("graph", "icon icon-sm")}Graph</a>
      <a class="tab" role="tab" aria-selected="${st.ftab === "board"}" href="#/f/${f.id}/board">${icon("filter", "icon icon-sm")}Board</a></div>`;

    main.innerHTML = `
      ${topbar(`<a class="cc-crumb-proj" href="#/" data-scope-link="${p.id}">${avatar(p)} ${p.name}</a><span class="sep cc-crumb-sep1">/</span><strong>${f.title}</strong>`,
        `${tabs("cc-top-tabs")}<a class="btn" href="#/f/${f.id}/launch">${icon("plus", "icon icon-sm")}<span class="btn-label">Launch${nLaunch ? ` · ${nLaunch}` : ""}</span></a>`)}
      <div class="cc-fview">
        <div class="cc-fhead">
          <h1>${f.title}</h1>
          <p>${p.name} · launched ${f.launched} · ${s.total} tickets in ${waves} waves<span class="cc-hint"> · <kbd>V</kbd> switches view</span></p>
          ${tabs("cc-ftabs-m")}
          <div class="cc-fstats"><div class="cc-seg">${seg(s)}</div>
            <span>${glyph("done")}${s.merged} merged</span><span>${glyph("attention")}${s.needs} need you</span><span>${glyph("running")}${s.active} in progress</span>${s.ready ? `<span>${glyph("ready")}${s.ready} ready</span>` : ""}<span>${glyph("blocked")}${s.blocked} blocked</span></div>
        </div>
        ${st.ftab === "graph" ? graph : groupedBoard(ts)}
      </div>`;
    if (st.ftab === "graph") requestAnimationFrame(drawWires);
  }

  function drawWires() {
    const g = main.querySelector(".cc-graph");
    if (!g) return;
    const svg = g.querySelector(".cc-wires");
    svg.setAttribute("width", g.scrollWidth);
    svg.setAttribute("height", g.scrollHeight);
    const paths = [];
    g.querySelectorAll(".cc-node").forEach((el) => {
      (byKey(el.dataset.node).blockedBy || []).forEach((k) => {
        const from = g.querySelector(`[data-node="${CSS.escape(k)}"]`);
        if (!from) return;
        const x1 = from.offsetLeft + from.offsetWidth, y1 = from.offsetTop + from.offsetHeight / 2;
        const x2 = el.offsetLeft, y2 = el.offsetTop + el.offsetHeight / 2, dx = (x2 - x1) / 2;
        paths.push(`<path class="${byKey(k).s === "done" ? "is-done" : ""}" data-from="${k}" data-to="${el.dataset.node}" d="M${x1} ${y1} C${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}"/>`);
      });
    });
    svg.innerHTML = paths.join("");
  }

  function trace(key) {
    const g = main.querySelector(".cc-graph");
    if (!g) return;
    g.classList.toggle("is-tracing", !!key);
    g.querySelectorAll(".is-lit").forEach((el) => el.classList.remove("is-lit"));
    if (!key) return;
    const t = byKey(key);
    [key, ...(t.blockedBy || []), ...unlocks(t).map((u) => u.key)].forEach((k) => g.querySelector(`.cc-node[data-node="${CSS.escape(k)}"]`)?.classList.add("is-lit"));
    g.querySelectorAll(`path[data-from="${CSS.escape(key)}"], path[data-to="${CSS.escape(key)}"]`).forEach((p) => p.classList.add("is-lit"));
  }

  function renderMain() {
    const page = pages[st.page] || renderSession;
    page();
    main.scrollTop = 0;
  }

  function renderOverlay() {
    if (st.overlay === "keys") overlay.innerHTML = pages.keys();
    else if (st.launch !== null) overlay.innerHTML = pages.launch();
    else overlay.innerHTML = "";
    app.classList.toggle("has-overlay", !!overlay.innerHTML);
    const focusable = overlay.querySelector("[autofocus], .cc-dialog button, .cc-dialog a");
    if (focusable) focusable.focus({ preventScroll: true });
  }

  function render() {
    renderRail();
    renderMain();
    renderOverlay();
  }

  function route() {
    const [path, qs] = here.split("?");
    st.v = new URLSearchParams(qs).get("v") || "A";
    const parts = path.replace(/^#\/?/, "").split("/").filter(Boolean).map(decodeURIComponent);
    const [head, ...rest] = parts;
    st.launch = null;
    st.overlay = null;
    if (head === "s" && byKey(rest.join("/"))) { st.page = "session"; st.sel = rest.join("/"); }
    else if (head === "f" && feature(rest[0])) {
      st.page = "feature";
      st.fid = rest[0];
      if (rest[1] === "launch") st.launch = rest[0];
      else if (rest[1] === "graph" || rest[1] === "board") st.ftab = rest[1];
      st.open.add(rest[0]);
    }
    else if (head === "launch") { st.launch = ""; if (!["feature", "session"].includes(st.page)) st.page = "session"; }
    else if (head === "tickets") st.page = "tickets";
    else if (head === "insights") st.page = "insights";
    else if (head === "repos" && rest.length === 2) { st.page = "repo"; st.repo = rest.join("/"); }
    else if (head === "repos") st.page = "repos";
    else { st.page = "session"; if (!byKey(st.sel)) st.sel = firstNeeds().key; }

    const home = !head;
    app.dataset.view = home ? "rail" : "main";
    if (st.page === "session") {
      const t = byKey(st.sel);
      st.open.add(t.feature);
      if (t.s === "blocked") st.blockedOpen.add(t.feature);
    }
    if (!st.launch && st.launch !== "") st.lastHash = here;
    render();
  }

  function verb(name, t) {
    if (name === "Open PR") return toast(`Opening ${t.repo} PR ${t.pr} on GitHub`);
    if (name === "Open log") { st.logMode = "raw"; return go(sessionHash(t.key)); }
    if (name === "Refresh") return toast(`Refreshed ${t.ref} from GitHub`);
    const prev = { s: t.s, reason: t.reason, detail: t.detail, verbs: t.verbs, last: t.last, elapsed: t.elapsed };
    if (name === "Re-run" || name === "Resolve") { t.s = "running"; t.reason = "Agent running"; t.elapsed = 0; t.last = name === "Resolve" ? "Rebasing onto main and resolving conflicts." : "Re-running from the last green commit."; t.tokens = t.tokens || 0; t.verbs = ["Kill"]; }
    else if (name === "Kill") { t.s = "failed"; t.reason = "Killed"; t.detail = "Stopped by you"; t.verbs = ["Re-run"]; }
    else if (name === "Remove worktree") return toast(`Removed the worktree for ${t.ref}`);
    else return toast(`${name}: ${t.ref}`);
    render();
    toast(`${name} · ${t.ref} ${t.title}`, () => { Object.assign(t, prev); render(); });
  }

  function settle(t) {
    st.settledKeys.add(t.key);
    const next = TICKETS.find((x) => NEEDS_YOU.has(x.s) && !isSettled(x) && inScope(x));
    if (next) go(sessionHash(next.key)); else render();
    toast(`Settled ${t.ref}`, () => { st.settledKeys.delete(t.key); go(sessionHash(t.key)); });
  }

  function closeOverlay() {
    if (st.overlay) { st.overlay = null; return renderOverlay(); }
    if (st.launch !== null) go(st.page === "feature" ? `#/f/${st.fid}/${st.ftab}` : st.lastHash);
  }

  function bind() {
    app.addEventListener("click", (e) => {
      const tgt = e.target;
      if (tgt.closest("[data-close]") || tgt.matches(".cc-scrim")) return closeOverlay();
      const v = tgt.closest("[data-verb]");
      if (v) { e.preventDefault(); const k = tgt.closest("[data-key]"); return verb(v.dataset.verb, k ? byKey(k.dataset.key) : byKey(st.sel)); }
      const sc = tgt.closest("[data-scope]");
      if (sc) { st.scope = sc.dataset.scope; st.menu = false; return render(); }
      const sl = tgt.closest("[data-scope-link]");
      if (sl) { st.scope = sl.dataset.scopeLink; }
      const act = tgt.closest("[data-act]");
      if (act) {
        const a = act.dataset.act;
        if (a === "scope") { st.menu = !st.menu; return renderRail(); }
        if (a === "settled") { st.settledOpen = !st.settledOpen; return renderRail(); }
        if (a === "panel") { app.classList.toggle(matchMedia("(max-width: 1180px)").matches ? "panel-over" : "no-panel"); return; }
        if (a === "settle") return settle(byKey(st.sel));
        if (a === "new") return go("#/launch");
        if (a === "keys") { st.overlay = "keys"; return renderOverlay(); }
        if (pages.onAct) pages.onAct(a, act, e);
        return;
      }
      const tg = tgt.closest("[data-toggle]");
      if (tg) { const id = tg.dataset.toggle; st.open.has(id) ? st.open.delete(id) : st.open.add(id); return renderRail(); }
      const lm = tgt.closest("[data-logmode]");
      if (lm) { st.logMode = lm.dataset.logmode; return renderMain(); }
      const bo = tgt.closest("[data-blocked]");
      if (bo) { const id = bo.dataset.blocked; st.blockedOpen.has(id) ? st.blockedOpen.delete(id) : st.blockedOpen.add(id); return renderRail(); }
      const d = tgt.closest("[data-disclose]");
      if (d) { const o = d.getAttribute("aria-expanded") !== "true"; d.setAttribute("aria-expanded", o); d.nextElementSibling.hidden = !o; return; }
      const k = tgt.closest("[data-key]");
      if (k && rail.contains(k)) return go(sessionHash(k.dataset.key));
      if (st.menu && !tgt.closest(".cc-scope")) { st.menu = false; renderRail(); }
    });

    app.addEventListener("change", (e) => {
      if (e.target.matches("[data-arm]")) {
        const t = byKey(st.sel);
        t.armed = e.target.checked;
        renderRail();
        toast(t.armed ? `${t.ref} will launch when its blockers merge` : `${t.ref} disarmed`);
      }
      if (pages.onChange) pages.onChange(e);
    });

    app.addEventListener("input", (e) => {
      if (e.target.matches("[data-search]")) {
        st.q = e.target.value;
        renderRail();
        const inp = rail.querySelector("[data-search]");
        inp.focus();
        inp.setSelectionRange(st.q.length, st.q.length);
      }
      if (pages.onInput) pages.onInput(e);
    });

    app.addEventListener("keydown", (e) => {
      if (e.target.matches("[data-search]") && e.key === "Enter") {
        const first = rail.querySelector("[data-key]");
        if (first) { go(sessionHash(first.dataset.key)); e.target.blur(); }
      }
      if (e.target.matches(".cc-row, .cc-card") && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); go(sessionHash(e.target.dataset.key)); }
    });

    app.addEventListener("submit", (e) => {
      e.preventDefault();
      if (pages.onSubmit && pages.onSubmit(e) !== false) return;
      const ta = e.target.querySelector("textarea");
      if (!ta.value.trim()) return ta.focus();
      toast(`Sent to the agent on ${byKey(st.sel).ref}`);
      ta.value = "";
    });

    main.addEventListener("pointerover", (e) => { const n = e.target.closest(".cc-node"); if (n) trace(n.dataset.node); });
    main.addEventListener("pointerout", (e) => { if (e.target.closest(".cc-node") && !e.relatedTarget?.closest?.(".cc-node")) trace(null); });
    addEventListener("resize", () => { if (st.page === "feature" && st.ftab === "graph") drawWires(); });

    let chord = null;
    document.addEventListener("keydown", (e) => {
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key.toLowerCase() === "k") { e.preventDefault(); const s = rail.querySelector("[data-search]"); app.dataset.view = "rail"; s.focus(); s.select(); return; }
      if (mod && /^[0-9]$/.test(e.key)) { const p = e.key === "0" ? "all" : PROJECTS[Number(e.key) - 1]?.id; if (p) { e.preventDefault(); st.scope = p; render(); } return; }
      if (CC.typing(e)) { if (e.key === "Escape") e.target.blur(); return; }
      if (mod || e.altKey) return;
      if (e.key === "Escape") { if (st.overlay || st.launch !== null) return closeOverlay(); st.menu = false; return renderRail(); }
      if (st.overlay || st.launch !== null) return;
      if (chord) {
        const to = { h: "#/", t: "#/tickets", i: "#/insights", r: "#/repos" }[e.key];
        chord = null;
        if (to) { e.preventDefault(); return go(to); }
      }
      const keys = [...rail.querySelectorAll("[data-key]")].map((el) => el.dataset.key);
      const i = keys.indexOf(st.sel);
      const t = byKey(st.sel);
      if (e.key === "g") { chord = true; setTimeout(() => { chord = null; }, 900); }
      else if (e.key === "j" || e.key === "ArrowDown") { e.preventDefault(); const k = keys[i + 1] || keys[0]; if (k) go(sessionHash(k)); }
      else if (e.key === "k" || e.key === "ArrowUp") { e.preventDefault(); const k = keys[Math.max(0, i - 1)]; if (k) go(sessionHash(k)); }
      else if (e.key === "c") go(st.page === "feature" ? `#/f/${st.fid}/launch` : "#/launch");
      else if (e.key === "?") { st.overlay = "keys"; renderOverlay(); }
      else if (st.page === "feature" && e.key === "v") go(`#/f/${st.fid}/${st.ftab === "graph" ? "board" : "graph"}`);
      else if (st.page !== "session") return;
      else if (e.key === "f") go(`#/f/${t.feature}/${st.ftab}`);
      else if (e.key === "e" && NEEDS_YOU.has(t.s)) settle(t);
      else if (e.key === "r" && t.verbs && t.verbs.includes("Re-run")) verb("Re-run", t);
      else if (e.key === "o" && t.pr) verb("Open PR", t);
      else if (e.key === "Enter") main.querySelector("textarea")?.focus();
    });

    const fromUrl = () => { here = location.hash || "#/"; route(); };
    addEventListener("hashchange", fromUrl);
    addEventListener("popstate", fromUrl);
    document.addEventListener("click", (e) => {
      const a = e.target.closest('a[href^="#"]');
      if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.button) return;
      e.preventDefault();
      go(a.getAttribute("href") === "#" ? here : a.getAttribute("href"));
    });
  }

  function start(root) {
    root.innerHTML = `<div class="cc-app" data-view="rail">
      <aside class="cc-rail" aria-label="Sessions"></aside>
      <main class="cc-main" id="main"></main>
      <div class="cc-overlay"></div>
    </div>`;
    app = root.firstElementChild;
    rail = app.querySelector(".cc-rail");
    main = app.querySelector(".cc-main");
    overlay = app.querySelector(".cc-overlay");
    pages.session = renderSession;
    pages.feature = renderFeature;
    bind();
    route();
    CC.tick();
  }

  return {
    st, pages, go, render, renderRail, renderMain, renderOverlay, start, topbar, groupedBoard, boardRow, launchable, seg, sessionHash, verb,
    get main() { return main; },
  };
})();
