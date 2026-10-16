(() => {
  const { TICKETS, PROJECTS, icon, glyph, avatar, toast, project } = CC;
  const { st, pages, topbar } = APP;
  const main = () => APP.main;
  const pct = (v) => `${v.toFixed(1)}%`;
  const TODAY = new Date(2026, 9, 16);
  const dayDate = (s) => new Date(`${s} 2026`);
  const NO_PROJECT = { id: null, name: "No project", hue: 0 };
  const projOf = (id) => project(id) || NO_PROJECT;

  Object.assign(st, { irange: 30, ifeature: "", ledgerOpen: new Set(), rfilter: "all", proj: PROJECTS[0].id, projEdit: false });

  function inRange(m) { return (TODAY - dayDate(m.day)) / 864e5 <= st.irange && (!st.ifeature || m.feature === st.ifeature); }
  const merged = () => CC.MERGED.filter(inRange).map((m) => ({ ...m, total: m.agent + m.follow }));
  const waste = () => CC.WASTE.tickets.filter((w) => !st.ifeature || w.feature === st.ifeature);

  function filters() {
    const feats = [...new Set(CC.MERGED.map((m) => m.feature))];
    return `<div class="cc-filters">
      <div class="tabs" role="tablist" aria-label="Range">${[7, 14, 30].map((n) => `<button class="tab" role="tab" type="button" aria-selected="${st.irange === n}" data-act="irange" data-v="${n}">${n} days</button>`).join("")}</div>
      <label class="cc-select"><span class="sr-only">Feature</span><select data-ifeature>
        <option value="">All features</option>${feats.map((f) => `<option ${st.ifeature === f ? "selected" : ""}>${f}</option>`).join("")}</select>${icon("down", "icon icon-sm")}</label>
    </div>`;
  }

  function limitCells() {
    const u = CC.USAGE;
    return `<div class="cc-limit-cells">${[["Five-hour window", u.fiveHour], ["Weekly", u.weekly]].map(([l, g]) => `
      <div class="cc-limit-cell"><div class="cc-limit-cell-top"><span>${l}</span><b>${g.pct}%</b></div>
        <div class="meter" role="meter" aria-valuenow="${g.pct}" aria-valuemin="0" aria-valuemax="100" aria-label="${l}"><span style="width:${g.pct}%"></span></div>
        <span class="meta">resets ${g.resets}</span></div>`).join("")}</div>`;
  }

  function ledger() {
    const rows = merged(), w = waste();
    const byF = new Map();
    rows.forEach((r) => {
      const g = byF.get(r.feature) || { feature: r.feature, project: r.project, items: [], agent: 0, follow: 0 };
      g.items.push(r); g.agent += r.agent; g.follow += r.follow;
      byF.set(r.feature, g);
    });
    const groups = [...byF.values()].map((g) => ({ ...g, total: g.agent + g.follow })).sort((a, b) => b.total - a.total);
    const wasteTotal = w.reduce((n, x) => n + x.pct, 0);
    const sum = (k) => groups.reduce((n, g) => n + g[k], 0);
    const total = sum("total"), max = Math.max(wasteTotal, ...groups.map((g) => g.total), 0.1);
    const bar = (a, f) => `<span class="cc-lbar" aria-hidden="true"><i style="width:${(a / max) * 100}%"></i>${f ? `<i class="is-follow" style="width:${(f / max) * 100}%"></i>` : ""}</span>`;
    const tr = (g) => {
      const open = st.ledgerOpen.has(g.feature);
      return `<tr class="cc-lrow">
          <th scope="row"><button class="cc-ltoggle" type="button" aria-expanded="${open}" data-act="ledger" data-v="${g.feature}">${icon("chev", "icon icon-sm")}${avatar(projOf(g.project))}${g.feature}</button></th>
          <td class="num">${g.items.length}</td><td class="num">${pct(g.agent)}</td><td class="num">${g.follow ? pct(g.follow) : "—"}</td><td class="num"><b>${pct(g.total)}</b></td><td class="cc-lbar-cell">${bar(g.agent, g.follow)}</td></tr>
        ${open ? g.items.map((r) => `<tr class="cc-lsub"><th scope="row"><span>${r.title}</span><span class="meta">${r.day}</span></th><td></td><td class="num">${pct(r.agent)}</td><td class="num">${r.follow ? pct(r.follow) : "—"}</td><td class="num">${pct(r.total)}</td><td class="cc-lbar-cell">${bar(r.agent, r.follow)}</td></tr>`).join("") : ""}`;
    };
    const wOpen = st.ledgerOpen.has("__waste");
    main().innerHTML = `
      ${topbar("<strong>Insights</strong>")}
      <div class="cc-page cc-page-narrow">
        <div class="cc-page-head"><h1>Insights</h1><p>What each feature's merged work cost, against your weekly limit.</p></div>
        ${filters()}
        ${limitCells()}
        <p class="cc-summary">${rows.length ? `${rows.length} merged ticket${rows.length > 1 ? "s" : ""} used <b>${pct(total)}</b> of a week${sum("follow") ? `, <b>${pct(sum("follow"))}</b> of it on follow-up runs` : ""}.` : "No merged tickets in this range."}${wasteTotal ? ` Withdrawn tickets wasted <b>${pct(wasteTotal)}</b>.` : ""}</p>
        <div class="cc-table-wrap"><table class="cc-table cc-ledger">
          <thead><tr><th>Feature</th><th class="num">Tickets</th><th class="num">Agent</th><th class="num">Follow-up</th><th class="num">Total</th><th><span class="cc-legend cc-legend-inline"><span><i class="cc-key-bar"></i>Agent</span><span><i class="cc-key-bar is-follow"></i>Follow-up</span></span></th></tr></thead>
          <tbody>${groups.map(tr).join("") || `<tr><td colspan="6" class="meta">No merged tickets in this range.</td></tr>`}
            ${wasteTotal ? `<tr class="cc-lrow cc-lwaste"><th scope="row"><button class="cc-ltoggle" type="button" aria-expanded="${wOpen}" data-act="ledger" data-v="__waste">${icon("chev", "icon icon-sm")}${glyph("failed")}Withdrawn tickets</button></th><td class="num">${w.length}</td><td class="num">—</td><td class="num">—</td><td class="num"><b>${pct(wasteTotal)}</b></td><td class="cc-lbar-cell"><span class="cc-lbar is-waste" aria-hidden="true"><i style="width:${(wasteTotal / max) * 100}%"></i></span></td></tr>
            ${wOpen ? w.map((x) => `<tr class="cc-lsub"><th scope="row"><span>${x.title}</span><span class="meta">${x.feature}</span></th><td></td><td></td><td></td><td class="num">${pct(x.pct)}</td><td></td></tr>`).join("") : ""}` : ""}
          </tbody>
          <tfoot><tr><th scope="row">Total</th><td class="num">${rows.length}</td><td class="num">${pct(sum("agent"))}</td><td class="num">${pct(sum("follow"))}</td><td class="num"><b>${pct(total + wasteTotal)}</b></td><td></td></tr></tfoot>
        </table></div>
      </div>`;
  }

  function timeline() {
    const days = CC.TOKENS_DAILY.slice(-st.irange), rows = merged();
    const W = 720, H = 236, L = 48, R = 8, T = 10, plotB = 176, rugY = 204;
    const top = Math.max(...days.map((d) => d.tin + d.tout));
    const step = top > 300000 ? 100000 : 50000, ymax = Math.ceil(top / step) * step;
    const bw = (W - L - R) / days.length, gap = Math.min(4, bw * 0.3);
    const y = (v) => plotB - (v / ymax) * (plotB - T);
    const k = (v) => (v >= 1e6 ? `${(v / 1e6).toFixed(1)}M` : `${Math.round(v / 1000)}k`);
    const ticks = Array.from({ length: ymax / step + 1 }, (_, i) => i * step).map((v) => `<line class="cc-grid${v ? "" : " is-base"}" x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}"/><text class="cc-tick" x="${L - 8}" y="${y(v)}" dy="0.32em" text-anchor="end">${k(v)}</text>`).join("");
    const bars = days.map((d, i) => {
      const x = L + i * bw + gap / 2, w = bw - gap, yi = y(d.tin), yo = y(d.tin + d.tout);
      const mergedThatDay = rows.filter((r) => r.day === d.day);
      return `<g><rect class="cc-bar-in" x="${x}" y="${yi}" width="${w}" height="${plotB - yi}" rx="2"/><rect class="cc-bar-out" x="${x}" y="${yo}" width="${w}" height="${Math.max(0, yi - yo - 2)}" rx="2"/>
        ${mergedThatDay.length ? `<circle class="cc-dot" cx="${x + w / 2}" cy="${rugY}" r="${3 + mergedThatDay.length}"/>` : ""}
        <rect class="cc-hit" x="${L + i * bw}" y="${T}" width="${bw}" height="${rugY + 10 - T}" data-day="${i}" tabindex="0" role="img" aria-label="${d.day}: ${k(d.tin)} input, ${k(d.tout)} output tokens${mergedThatDay.length ? `, ${mergedThatDay.length} merged` : ""}"/></g>`;
    }).join("");
    const xl = [0, Math.floor(days.length / 2), days.length - 1].map((i) => `<text class="cc-tick" x="${L + i * bw + bw / 2}" y="${H - 4}" text-anchor="middle">${days[i].day}</text>`).join("");
    const byDay = [...new Set(rows.map((r) => r.day))].reverse();
    const tin = days.reduce((n, d) => n + d.tin, 0), tout = days.reduce((n, d) => n + d.tout, 0);
    main().innerHTML = `
      ${topbar("<strong>Insights</strong>")}
      <div class="cc-page cc-page-narrow">
        <div class="cc-page-head"><h1>Insights</h1><p>Token use day by day, with the merges it produced.</p></div>
        ${filters()}
        ${limitCells()}
        <section class="cc-module" aria-labelledby="tok-h">
          <div class="cc-module-head"><h2 id="tok-h">Tokens per day</h2>
            <div class="cc-legend"><span><i class="cc-key-sq"></i>Input ${k(tin)}</span><span><i class="cc-key-sq is-out"></i>Output ${k(tout)}</span><span><i class="cc-key-dot"></i>Merges</span></div></div>
          <p class="cc-module-sub">Every number is read off the agents' own logs. Days with no runs stay at zero.</p>
          <div class="cc-chart" data-tchart><svg viewBox="0 0 ${W} ${H}" role="group" aria-label="Tokens per day, last ${st.irange} days">${ticks}<text class="cc-tick" x="${L - 8}" y="${rugY}" dy="0.32em" text-anchor="end">Merged</text>${bars}${xl}</svg><div class="cc-tip" hidden></div></div>
        </section>
        <section class="cc-module" aria-labelledby="mg-h">
          <h2 id="mg-h">Merged</h2>
          ${byDay.length ? `<div class="cc-daylist">${byDay.map((d) => `<div class="cc-day"><h3>${d}</h3>${rows.filter((r) => r.day === d).map((r) => `<div class="cc-dayrow">${glyph("done")}<span class="cc-dayrow-t">${r.title}</span><span class="cc-tfeat">${avatar(projOf(r.project))}${r.feature}</span><span class="mono">${pct(r.total)}</span></div>`).join("")}</div>`).join("")}</div>` : `<div class="cc-empty"><b>No merges in this range</b><span>Widen the range or pick another feature.</span></div>`}
        </section>
      </div>`;
    const box = main().querySelector("[data-tchart]"), tip = box.querySelector(".cc-tip");
    const show = (el) => {
      const d = days[Number(el.dataset.day)], m = rows.filter((r) => r.day === d.day);
      tip.innerHTML = `<b>${d.day}</b><dl><dt>Input</dt><dd>${k(d.tin)}</dd><dt>Output</dt><dd>${k(d.tout)}</dd></dl>${m.length ? `<span>Merged: ${m.map((r) => r.title).join(", ")}</span>` : ""}`;
      tip.hidden = false;
      const bb = box.getBoundingClientRect(), pb = el.getBoundingClientRect();
      tip.style.left = `${Math.min(Math.max(pb.left - bb.left + pb.width / 2 - tip.offsetWidth / 2, 0), bb.width - tip.offsetWidth)}px`;
      tip.style.top = `${Math.max(0, pb.top - bb.top - 8)}px`;
      el.parentNode.classList.add("is-on");
    };
    const hide = (el) => { tip.hidden = true; el.parentNode.classList.remove("is-on"); };
    box.addEventListener("pointerover", (e) => { if (e.target.dataset.day) show(e.target); });
    box.addEventListener("pointerout", (e) => { if (e.target.dataset.day) hide(e.target); });
    box.addEventListener("focusin", (e) => { if (e.target.dataset.day) show(e.target); });
    box.addEventListener("focusout", (e) => { if (e.target.dataset.day) hide(e.target); });
  }

  const RG = { ready: "done", refused: "failed", pending: "running" };
  const RL = { ready: "Ready", refused: "Refused", pending: "Checking" };
  const CG = { ok: "done", default: "done", fail: "failed", skip: "blocked", run: "running", wait: "blocked" };
  const CT = { ok: "Passed", default: "Missing, defaults in use", fail: "Failed", skip: "Not checked", run: "Running", wait: "Waiting" };
  const open = (n) => TICKETS.filter((t) => t.repo === n && t.s !== "done").length;
  const feats = (n) => CC.REPO_FEATURES[n] || [];

  const trackForm = () => `<form class="cc-track" data-form="track" autocomplete="off">
      <label class="sr-only" for="track-q">Track a repo</label>
      <div class="cc-track-field">${icon("search", "icon icon-sm")}<input id="track-q" class="input" placeholder="Track a repo: search owner/name" value="${st.trackQ || ""}" data-track></div>
      <p class="cc-help">Search runs as this machine's <span class="code">gh</span> identity.</p>
    </form>`;
  const importBanner = () => CC.IMPORT_ERROR ? `<div class="cc-note cc-note-warn">${glyph("failed")}<span>Import of <b>${CC.IMPORT_ERROR.feature}</b> refused ${CC.IMPORT_ERROR.age}. ${CC.IMPORT_ERROR.message}</span><button class="btn btn-ghost btn-sm" type="button" data-act="dismiss-import">Dismiss</button></div>` : "";
  const moveSelect = (r) => `<label class="cc-select cc-select-sm"><span class="sr-only">Project of ${r.name}</span><select data-move="${r.name}">
      ${PROJECTS.map((p) => `<option value="${p.id}" ${r.project === p.id ? "selected" : ""}>${p.name}</option>`).join("")}<option value="" ${r.project ? "" : "selected"}>No project</option></select>${icon("down", "icon icon-sm")}</label>`;

  function matrix() {
    const all = CC.REPOS.filter((r) => st.scope === "all" || r.project === st.scope);
    const F = [["all", "All", () => true], ["ready", "Ready", (r) => r.state === "ready"], ["refused", "Refused", (r) => r.state === "refused"], ["pending", "Checking", (r) => r.state === "pending"]];
    const rows = all.filter(F.find(([id]) => id === st.rfilter)[2]).sort((a, b) => ["refused", "pending", "ready"].indexOf(a.state) - ["refused", "pending", "ready"].indexOf(b.state));
    main().innerHTML = `
      ${topbar("<strong>Repos</strong>")}
      <div class="cc-page">
        <div class="cc-page-head"><h1>Repos</h1><p>Agents only launch in repos that pass all four checks. Refused repos sort to the top.</p></div>
        ${importBanner()}
        ${trackForm()}
        <div class="tabs cc-filter-tabs" role="tablist" aria-label="Filter repos">${F.map(([id, l, p]) => `<button class="tab" role="tab" type="button" aria-selected="${st.rfilter === id}" data-act="rfilter" data-v="${id}">${l}<span class="count">${all.filter(p).length}</span></button>`).join("")}</div>
        <div class="cc-table-wrap"><table class="cc-table cc-matrix">
          <thead><tr><th>Repo</th><th>Project</th>${CC.CHECKS.map(([, l]) => `<th class="cc-mcheck" title="${l}">${l.replace("Default branch is main", "Main branch").replace("Squash-only merges", "Squash-only").replace("Settings file", "Settings")}</th>`).join("")}<th class="num">Features</th><th class="num">Open</th></tr></thead>
          <tbody>${rows.map((r) => `<tr class="${r.state === "refused" ? "is-refused" : ""}">
            <th scope="row"><a class="cc-mrepo" href="#/repos/${r.name}">${glyph(RG[r.state], true)}<span><b class="mono">${r.name}</b><span class="${r.state === "refused" ? "cc-repo-why" : "meta"}">${r.state === "refused" ? r.reason : r.state === "pending" ? "Checking the repo on GitHub, then cloning it" : r.checks.settings === "default" ? "Defaults in use" : "Settings from .command-centre.toml"}</span></span></a></th>
            <td>${moveSelect(r)}</td>
            ${CC.CHECKS.map(([id, l]) => `<td class="cc-mcheck" title="${l}: ${r.checks[id] === "fail" ? r.reason : CT[r.checks[id]]}">${glyph(CG[r.checks[id]], true)}<span class="sr-only">${l}: ${CT[r.checks[id]]}</span></td>`).join("")}
            <td class="num">${feats(r.name).length || "—"}</td><td class="num">${r.state === "ready" ? open(r.name) : "—"}</td></tr>`).join("") || `<tr><td colspan="8" class="meta">No repos in this view.</td></tr>`}</tbody>
        </table></div>
      </div>`;
  }

  function projectsView() {
    const list = [...PROJECTS, NO_PROJECT];
    const sel = st.proj === "__new" ? null : (list.find((p) => p.id === st.proj) || PROJECTS[0]);
    const reposOf = (p) => CC.REPOS.filter((r) => (r.project || null) === p.id);
    const worst = (rs) => (rs.some((r) => r.state === "refused") ? "failed" : rs.some((r) => r.state === "pending") ? "running" : "done");
    const hues = (cur) => `<fieldset class="cc-hues"><legend class="sr-only">Colour</legend>${[1, 2, 3, 0].map((h) => `<label><input type="radio" name="hue" value="${h}" ${cur === h ? "checked" : ""}><span class="avatar" data-hue="${h}" aria-label="Colour ${h || "grey"}">${(sel?.name || "N")[0]}</span></label>`).join("")}</fieldset>`;
    let detail;
    if (!sel) {
      const pick = CC.REPOS.filter((r) => r.state !== "pending");
      detail = `<form class="cc-pform" data-form="proj-new">
        <h2>New project</h2>
        <div class="field"><label for="pn">Name</label><input id="pn" class="input" name="name" required maxlength="40" placeholder="e.g. side-projects" autofocus></div>
        <div class="field"><span class="cc-flabel">Colour</span>${hues(1)}</div>
        <fieldset class="cc-prepos"><legend>Repos</legend>${pick.map((r) => `<label><input type="checkbox" name="repo" value="${r.name}"><span class="mono">${r.name}</span><span class="meta">${projOf(r.project).name}</span></label>`).join("")}</fieldset>
        <div class="cc-dialog-actions"><button class="btn btn-ghost" type="button" data-act="proj" data-v="${PROJECTS[0].id}">Cancel</button><button class="btn btn-primary" type="submit">Create project</button></div>
      </form>`;
    } else {
      const rs = reposOf(sel), fs = new Set(rs.flatMap((r) => feats(r.name).map((f) => f.f)));
      detail = `<div class="cc-phead">
          <span class="avatar cc-avatar-lg" data-hue="${sel.hue}">${sel.name[0]}</span>
          <div><h2>${sel.name}</h2><p class="meta">${rs.length} repo${rs.length === 1 ? "" : "s"} · ${fs.size} feature${fs.size === 1 ? "" : "s"}</p></div>
          ${sel.id ? `<div class="cc-phead-act"><button class="btn btn-sm" type="button" data-act="proj-edit" aria-expanded="${st.projEdit}">Edit</button><button class="btn btn-sm btn-danger" type="button" data-act="proj-delete">Delete</button></div>` : ""}
        </div>
        ${st.projEdit && sel.id ? `<form class="cc-pedit" data-form="proj-edit"><div class="field"><label for="pe">Name</label><input id="pe" class="input" name="name" value="${sel.name}" required maxlength="40"></div><div class="field"><span class="cc-flabel">Colour</span>${hues(sel.hue)}</div><button class="btn btn-sm btn-primary" type="submit">Save</button></form>` : ""}
        ${!sel.id ? `<p class="cc-module-sub">Repos that aren't in a project yet. Move one into a project to group its features in the rail.</p>` : ""}
        <div class="cc-pcards">${rs.map((r) => `<article class="cc-pcard${r.state === "refused" ? " is-refused" : ""}">
          <header><a class="mono" href="#/repos/${r.name}">${r.name}</a><span class="badge" data-s="${RG[r.state]}">${glyph(RG[r.state])}${RL[r.state]}</span>${moveSelect(r)}</header>
          <ul class="cc-steps">${CC.CHECKS.map(([id, l]) => `<li>${glyph(CG[r.checks[id]], true)}<span>${l}</span></li>`).join("")}</ul>
          ${r.state === "refused" ? `<div class="cc-pfix"><p><b>${r.reason}.</b> ${r.fix}</p><button class="btn btn-sm btn-primary" type="button" data-act="retrack" data-repo="${r.name}">Track again</button></div>` : ""}
          ${feats(r.name).length ? `<div class="cc-pfeats">${feats(r.name).map((f) => `<div class="cc-pfeat"><span>${f.f}${f.also ? ` <span class="meta">also in ${f.also.join(", ")}</span>` : ""}</span><span class="meta">${f.imported ? "Imported" : "Not imported"}</span>${f.imported ? `<button class="btn btn-ghost btn-sm" type="button" data-act="reimport" data-v="${f.f}">Reimport</button>` : ""}<a class="btn btn-sm" href="#/launch">Launch</a></div>`).join("")}</div>` : r.state === "ready" ? `<p class="meta cc-pnone">No project labels found in this repo's tracker yet.</p>` : ""}
        </article>`).join("") || `<div class="cc-empty"><b>No repos here</b><span>Move a repo into ${sel.name} from its menu, or track a new one above.</span></div>`}</div>`;
    }
    main().innerHTML = `
      ${topbar("<strong>Repos</strong>")}
      <div class="cc-page">
        <div class="cc-page-head"><h1>Repos</h1><p>Projects group repos so their features share a colour and a scope in the rail.</p></div>
        ${importBanner()}
        ${trackForm()}
        <div class="cc-projects">
          <nav class="cc-plist" aria-label="Projects">
            ${list.map((p) => { const rs = reposOf(p); return rs.length || p.id ? `<button class="cc-pitem" type="button" data-act="proj" data-v="${p.id ?? ""}" aria-current="${!!sel && sel.id === p.id}">${avatar(p)}<span>${p.name}</span>${rs.length ? glyph(worst(rs)) : ""}<span class="count">${rs.length}</span></button>` : ""; }).join("")}
            <button class="cc-pitem cc-pnew" type="button" data-act="proj" data-v="__new" aria-current="${!sel}">${icon("plus", "icon icon-sm")}New project</button>
          </nav>
          <section class="cc-pdetail" aria-live="polite">${detail}</section>
        </div>
      </div>`;
  }

  const SETS = {
    insights: [["A", "Report", pages.insights], ["B", "Feature ledger", ledger], ["C", "Daily timeline", timeline]],
    repos: [["A", "Grouped list", pages.repos], ["B", "Health matrix", matrix], ["C", "Projects", projectsView]],
  };

  function run(page) {
    const set = SETS[page], i = Math.max(0, set.findIndex(([k]) => k === st.v)), [k, name, fn] = set[i];
    fn();
    const at = (d) => `#/${page}?v=${set[(i + d + set.length) % set.length][0]}`;
    main().insertAdjacentHTML("beforeend", `<nav class="cc-proto" aria-label="Prototype variant"><a href="${at(-1)}" aria-label="Previous variant">${icon("back", "icon icon-sm")}</a><span><b>${k}</b>${name}</span><a href="${at(1)}" aria-label="Next variant">${icon("chev", "icon icon-sm")}</a></nav>`);
  }
  pages.insights = () => run("insights");
  pages.repos = () => run("repos");

  document.addEventListener("keydown", (e) => {
    if (!SETS[st.page] || CC.typing(e) || e.metaKey || e.ctrlKey || e.altKey || (e.key !== "ArrowLeft" && e.key !== "ArrowRight")) return;
    main().querySelector(`.cc-proto a:${e.key === "ArrowLeft" ? "first-child" : "last-child"}`)?.click();
  });

  const baseAct = pages.onAct, baseChange = pages.onChange, baseSubmit = pages.onSubmit;
  pages.onAct = (a, el, e) => {
    const v = el.dataset.v;
    if (a === "irange") { st.irange = Number(v); return APP.renderMain(); }
    if (a === "ledger") { st.ledgerOpen.has(v) ? st.ledgerOpen.delete(v) : st.ledgerOpen.add(v); return APP.renderMain(); }
    if (a === "rfilter") { st.rfilter = v; return APP.renderMain(); }
    if (a === "proj") { st.proj = v === "" ? null : v; st.projEdit = false; return APP.renderMain(); }
    if (a === "proj-edit") { st.projEdit = !st.projEdit; return APP.renderMain(); }
    if (a === "proj-delete") {
      const p = project(st.proj);
      const live = CC.FEATURES.filter((f) => f.project === p.id && !f.settled).length;
      if (live) return toast(`${p.name} has ${live} feature${live === 1 ? "" : "s"} in flight. Settle them before deleting it.`);
      if (!confirm(`Delete project ${p.name}? Its repos stay tracked and become loose.`)) return;
      const moved = CC.REPOS.filter((r) => r.project === p.id);
      moved.forEach((r) => { r.project = null; });
      PROJECTS.splice(PROJECTS.indexOf(p), 1);
      st.proj = null;
      APP.render();
      return toast(`Deleted ${p.name}; ${moved.length} repo${moved.length === 1 ? "" : "s"} now loose`);
    }
    if (a === "reimport") return toast(`Reimporting ${v} from the tracker`);
    if (a === "dismiss-import") { CC.IMPORT_ERROR = null; return APP.renderMain(); }
    return baseAct && baseAct(a, el, e);
  };
  pages.onChange = (e) => {
    if (e.target.matches("[data-ifeature]")) { st.ifeature = e.target.value; return APP.renderMain(); }
    if (e.target.matches("[data-move]")) {
      const r = CC.REPOS.find((x) => x.name === e.target.dataset.move), prev = r.project;
      r.project = e.target.value || null;
      APP.render();
      return toast(`Moved ${r.name} to ${projOf(r.project).name}`, () => { r.project = prev; APP.render(); });
    }
    return baseChange && baseChange(e);
  };
  pages.onSubmit = (e) => {
    const f = e.target, kind = f.dataset.form;
    if (kind === "proj-new" || kind === "proj-edit") {
      const data = new FormData(f), name = data.get("name").trim().toLowerCase().replace(/\s+/g, "-"), hue = Number(data.get("hue") || 0);
      if (kind === "proj-new") {
        if (PROJECTS.some((p) => p.id === name)) { toast(`A project called ${name} already exists`); return true; }
        PROJECTS.push({ id: name, name, hue });
        data.getAll("repo").forEach((n) => { CC.REPOS.find((r) => r.name === n).project = name; });
        st.proj = name;
        toast(`Created ${name}`);
      } else {
        Object.assign(project(st.proj), { name, hue });
        st.projEdit = false;
        toast(`Saved ${name}`);
      }
      APP.render();
      return true;
    }
    return baseSubmit ? baseSubmit(e) : false;
  };
})();
