(() => {
  const { PROJECTS, FEATURES, icon, glyph, avatar, project } = CC;
  const { st, pages, topbar } = APP;
  const main = () => APP.main;

  const TODAY = new Date(2026, 9, 16);
  const dayDate = (s) => new Date(`${s} 2026`);
  const ago = (s) => Math.round((TODAY - dayDate(s)) / 864e5);
  const pct = (v) => `${v.toFixed(1)}%`;
  const share = (a, b) => (b ? Math.round((a / b) * 100) : 0);
  const hm = (m) => (m >= 60 ? `${Math.floor(m / 60)}h ${String(Math.round(m % 60)).padStart(2, "0")}m` : `${Math.round(m)}m`);
  const k = (v) => (v >= 1e6 ? `${(v / 1e6).toFixed(1)}M` : `${Math.round(v / 1000)}k`);
  const median = (xs) => { if (!xs.length) return 0; const s = [...xs].sort((a, b) => a - b), m = s.length >> 1; return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2; };
  const sum = (xs, f) => xs.reduce((n, x) => n + f(x), 0);

  const OLDER = [
    { title: "Add the health endpoint", feature: "Observability", project: "acme", day: "Sep 18", agent: 0.6, follow: 0 },
    { title: "Scaffold the metrics package", feature: "Observability", project: "acme", day: "Sep 19", agent: 1.0, follow: 0.3 },
    { title: "Wire structured logging", feature: "Observability", project: "acme", day: "Sep 22", agent: 1.4, follow: 0.5 },
    { title: "Golden-test the greenhouse adapter", feature: "Testing abstractions", project: "job-scraper", day: "Sep 24", agent: 0.7, follow: 0 },
    { title: "Golden-test the lever adapter", feature: "Testing abstractions", project: "job-scraper", day: "Sep 25", agent: 0.5, follow: 0 },
    { title: "Usage meter data model", feature: "Usage meters", project: "command-center", day: "Sep 27", agent: 1.2, follow: 0.6 },
  ];
  const CHECKS = ["lint", "unit", "build", "integration"];
  const runs = [...OLDER, ...CC.MERGED].map((m, i) => {
    const n = (a, b) => a + ((i * 37 + b * 11) % 7) * b;
    const rerun = (i * 5) % 7 === 0 ? 1 : 0, resolve = i % 6 === 2 ? 1 : 0, steer = i % 4 === 1 ? 1 : 0;
    const firstGreen = i % 3 !== 0;
    const tokIn = Math.round((m.agent + m.follow) * 150000 + n(20000, 9000));
    return {
      ...m, total: m.agent + m.follow, outcome: "merged",
      runMin: n(8, 6), checkMin: n(4, 2), pushMin: n(1, 1), reviewMin: n(40, 38),
      rerun, resolve, kill: 0, steer, followRuns: m.follow ? 1 : 0, firstGreen, failedCheck: firstGreen ? null : CHECKS[i % 2 ? 0 : 3],
      tokIn, tokOut: Math.round(tokIn * 0.19), turns: n(6, 3),
      tools: { bash: [n(18, 5), i % 3 ? 1 : 3], edit: [n(10, 3), i % 4 ? 0 : 1], read: [n(24, 6), 0], grep: [n(7, 2), i % 5 ? 0 : 1] },
    };
  });
  const withdrawn = CC.WASTE.tickets.map((w, i) => ({ ...w, project: w.feature === "Billing" ? "acme" : "acme", day: ["Oct 3", "Oct 11"][i], total: w.pct, outcome: "withdrawn", tokIn: 210000 + i * 90000, tokOut: 40000, turns: 18 + i * 6, tools: { bash: [40, 9], edit: [12, 2], read: [30, 0], grep: [8, 1] } }));

  Object.assign(st, { irange: 14, iproj: "", ifeat: "", itab: "overview", iall: new Set() });

  function inPeriod(r, offset = 0) {
    const d = ago(r.day) - offset * st.irange;
    return d >= 0 && d < st.irange && (!st.iproj || r.project === st.iproj) && (!st.ifeat || r.feature === st.ifeat);
  }
  const period = (offset = 0) => ({ merged: runs.filter((r) => inPeriod(r, offset)), withdrawn: withdrawn.filter((r) => inPeriod(r, offset)) });

  function stats(p) {
    const done = p.merged.length + p.withdrawn.length;
    const clean = p.merged.filter((r) => !r.rerun && !r.resolve && !r.followRuns && !r.steer).length;
    return {
      merged: p.merged.length,
      mergeRate: done ? p.merged.length / done : null,
      ttm: p.merged.length ? median(p.merged.map((r) => r.runMin + r.checkMin + r.pushMin + r.reviewMin)) : null,
      cost: p.merged.length ? median(p.merged.map((r) => r.total)) : null,
      autonomy: p.merged.length ? clean / p.merged.length : null,
      spend: sum(p.merged, (r) => r.total) + sum(p.withdrawn, (r) => r.total),
    };
  }

  function delta(now, prev, { lowerIsBetter = false, fmt }) {
    if (now == null || prev == null || prev === 0) return `<span class="ci-delta">no earlier data</span>`;
    const d = now - prev;
    if (Math.abs(d) < 1e-9) return `<span class="ci-delta">no change</span>`;
    const good = lowerIsBetter ? d < 0 : d > 0;
    return `<span class="ci-delta ${good ? "is-good" : "is-bad"}">${d > 0 ? "+" : "−"}${fmt(Math.abs(d))} vs previous ${st.irange} days</span>`;
  }

  function headline(s, prev) {
    const tiles = [
      ["Merged", s.merged, prev.merged, (v) => String(v), {}, "tickets"],
      ["Merge rate", s.mergeRate, prev.mergeRate, (v) => `${Math.round(v * 100)}%`, {}, "merged vs withdrawn"],
      ["Time to merge", s.ttm, prev.ttm, hm, { lowerIsBetter: true }, "median, launch to merge"],
      ["Cost per merge", s.cost, prev.cost, pct, { lowerIsBetter: true }, "median share of a week"],
    ];
    return `<div class="ci-strip">${tiles.map(([l, v, p, f, o, sub]) => `<div class="ci-stat"><span class="ci-stat-l">${l}</span><b>${v == null ? "—" : f(v)}</b><span class="ci-stat-sub">${sub}</span>${delta(v, p, { ...o, fmt: f })}</div>`).join("")}</div>`;
  }

  function limits() {
    const u = CC.USAGE, dayOfWeek = 3.5, projected = Math.round((u.weekly.pct / dayOfWeek) * 7);
    return `<section class="cc-module" aria-labelledby="lim-h"><h2 id="lim-h">Limits</h2>
      <div class="cc-limits">
        <div class="cc-limit"><span>Five-hour window</span><div class="meter"><span style="width:${u.fiveHour.pct}%"></span></div><b>${u.fiveHour.pct}%</b><span class="meta">resets ${u.fiveHour.resets}</span></div>
        <div class="cc-limit"><span>Weekly</span><div class="meter ci-meter-proj"><span style="width:${u.weekly.pct}%"></span><i style="left:${Math.min(projected, 100)}%" title="Projected at reset"></i></div><b>${u.weekly.pct}%</b><span class="meta">≈${projected}% at reset ${u.weekly.resets}</span></div>
      </div></section>`;
  }

  function observations(p) {
    const out = [];
    const m = p.merged;
    if (m.length) {
      const fails = CHECKS.map((c) => [c, m.filter((r) => r.failedCheck === c).length]).sort((a, b) => b[1] - a[1])[0];
      if (fails[1]) out.push(["failed", `<b>${fails[0]}</b> failed the first push on ${fails[1]} of ${m.length} merged tickets.`, "quality"]);
      const review = median(m.map((r) => r.reviewMin)), agent = median(m.map((r) => r.runMin));
      if (review > agent) out.push(["attention", `PRs wait <b>${hm(review)}</b> for your review on median, ${Math.round(review / agent)}× longer than agents take to write them.`, "flow"]);
      const top = Object.entries(m.reduce((a, r) => ({ ...a, [r.feature]: (a[r.feature] || 0) + r.total }), {})).sort((a, b) => b[1] - a[1])[0];
      out.push(["running", `<b>${top[0]}</b> used the most of your limit: ${pct(top[1])} of a week across its merges.`, "spend"]);
    }
    if (p.withdrawn.length) out.push(["blocked", `${p.withdrawn.length} withdrawn ticket${p.withdrawn.length > 1 ? "s" : ""} wasted <b>${pct(sum(p.withdrawn, (r) => r.total))}</b> of a week.`, "spend"]);
    return out.length ? `<section class="cc-module" aria-labelledby="obs-h"><h2 id="obs-h">Worth a look</h2><ul class="ci-obs">${out.map(([s, t, tab]) => `<li>${glyph(s)}<span>${t}</span><button class="btn btn-ghost btn-sm" type="button" data-act="itab" data-v="${tab}">See ${tab}</button></li>`).join("")}</ul></section>` : "";
  }

  function dotChart(rows) {
    if (!rows.length) return empty();
    const W = 720, H = 220, L = 44, R = 16, T = 12, B = 28;
    const ymax = Math.max(1, Math.ceil(Math.max(...rows.map((r) => r.total), CC.WASTE.pct)));
    const x = (i) => L + (rows.length === 1 ? (W - L - R) / 2 : (i * (W - L - R)) / (rows.length - 1));
    const y = (v) => T + (1 - v / ymax) * (H - T - B);
    const med = rows.map((_, i) => median(rows.slice(Math.max(0, i - 9), i + 1).map((r) => r.total)));
    return `<div class="cc-chart"><svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Spend per merged ticket, ${rows.length} tickets">
      ${Array.from({ length: ymax + 1 }, (_, v) => `<line class="cc-grid${v ? "" : " is-base"}" x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}"/><text class="cc-tick" x="${L - 8}" y="${y(v)}" dy="0.32em" text-anchor="end">${v}%</text>`).join("")}
      <text class="cc-tick" x="${x(0)}" y="${H - 6}">${rows[0].day}</text><text class="cc-tick" x="${x(rows.length - 1)}" y="${H - 6}" text-anchor="end">${rows[rows.length - 1].day}</text>
      <line class="cc-waste" x1="${L}" x2="${W - R}" y1="${y(CC.WASTE.pct)}" y2="${y(CC.WASTE.pct)}"><title>Waste from withdrawn tickets</title></line>
      <path class="cc-median" d="${med.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)} ${y(v).toFixed(1)}`).join(" ")}"/>
      ${rows.map((r, i) => `<circle class="cc-dot" cx="${x(i)}" cy="${y(r.total)}" r="4"><title>${r.title} · ${r.feature} · ${r.day}: ${pct(r.total)} (agent ${pct(r.agent)}, follow-up ${pct(r.follow)})</title></circle>`).join("")}
    </svg></div>`;
  }

  function tokenBars() {
    const days = CC.TOKENS_DAILY.slice(-st.irange);
    const W = 720, H = 180, L = 48, R = 8, T = 10, B = 26;
    const top = Math.max(...days.map((d) => d.tin + d.tout)), step = 100000, ymax = Math.ceil(top / step) * step;
    const bw = (W - L - R) / days.length, gap = Math.min(4, bw * 0.3), y = (v) => H - B - (v / ymax) * (H - T - B);
    return `<div class="cc-chart"><svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Tokens per day">
      ${[0, ymax / 2, ymax].map((v) => `<line class="cc-grid${v ? "" : " is-base"}" x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}"/><text class="cc-tick" x="${L - 8}" y="${y(v)}" dy="0.32em" text-anchor="end">${k(v)}</text>`).join("")}
      ${days.map((d, i) => { const xx = L + i * bw + gap / 2, w = bw - gap; return `<g><title>${d.day}: ${k(d.tin)} input, ${k(d.tout)} output</title><rect class="cc-bar-in" x="${xx}" y="${y(d.tin)}" width="${w}" height="${H - B - y(d.tin)}" rx="2"/><rect class="cc-bar-out" x="${xx}" y="${y(d.tin + d.tout)}" width="${w}" height="${Math.max(0, y(d.tin) - y(d.tin + d.tout) - 2)}" rx="2"/></g>`; }).join("")}
      <text class="cc-tick" x="${L}" y="${H - 6}">${days[0].day}</text><text class="cc-tick" x="${W - R}" y="${H - 6}" text-anchor="end">${days[days.length - 1].day}</text>
    </svg></div>`;
  }

  function barList(rows, { fmt = (v) => v, max, tone = "" } = {}) {
    const top = max || Math.max(...rows.map((r) => r.v), 1);
    return `<ul class="ci-bars">${rows.map((r) => `<li><span class="ci-bar-l">${r.l}</span><span class="ci-bar ${r.tone || tone}"><i style="width:${(r.v / top) * 100}%"></i></span><span class="ci-bar-v">${fmt(r.v)}${r.sub ? ` <span class="meta">${r.sub}</span>` : ""}</span></li>`).join("")}</ul>`;
  }

  function capped(id, rows, render, n = 6) {
    const all = st.iall.has(id);
    return `${render(all ? rows : rows.slice(0, n))}${rows.length > n ? `<button class="btn btn-ghost btn-sm ci-more" type="button" data-act="iall" data-v="${id}">${all ? "Show fewer" : `Show all ${rows.length}`}</button>` : ""}`;
  }

  const empty = () => `<div class="cc-empty"><b>Nothing in this range</b><span>Widen the range or clear the filters.</span></div>`;
  const mod = (id, title, sub, body, legend = "") => `<section class="cc-module" aria-labelledby="${id}"><div class="cc-module-head"><h2 id="${id}">${title}</h2>${legend}</div>${sub ? `<p class="cc-module-sub">${sub}</p>` : ""}${body}</section>`;
  const legend = (items) => `<div class="cc-legend">${items.map(([cls, l]) => `<span><i class="${cls}"></i>${l}</span>`).join("")}</div>`;

  const SEG = [["runMin", "Agent running", "ci-seg-run"], ["checkMin", "Checks", "ci-seg-check"], ["pushMin", "Push", "ci-seg-push"], ["reviewMin", "Waiting on you", "ci-seg-review"]];

  const TABS = {
    overview(p, s, prev) {
      return headline(s, prev) + limits() + observations(p)
        + mod("sp-h", "Spend per merged ticket", "Share of the weekly limit each ticket used, from launch to merge.", dotChart(p.merged), legend([["cc-key-dot", "Ticket"], ["cc-key-line", "Rolling median of 10"], ["cc-key-waste", "Waste"]]));
    },
    spend(p) {
      const byFeat = Object.values(p.merged.reduce((a, r) => { (a[r.feature] ||= { l: r.feature, v: 0, n: 0, project: r.project }).v += r.total; a[r.feature].n++; return a; }, {})).sort((a, b) => b.v - a.v);
      const tin = sum(CC.TOKENS_DAILY.slice(-st.irange), (d) => d.tin), tout = sum(CC.TOKENS_DAILY.slice(-st.irange), (d) => d.tout);
      return mod("sp1", "Spend per merged ticket", "Share of the weekly limit each ticket used, from launch to merge.", dotChart(p.merged), legend([["cc-key-dot", "Ticket"], ["cc-key-line", "Rolling median of 10"], ["cc-key-waste", "Waste"]]))
        + mod("sp2", "Tokens per day", `${k(tin)} input and ${k(tout)} output over the last ${st.irange} days.`, tokenBars(), legend([["cc-key-sq", "Input"], ["cc-key-sq is-out", "Output"]]))
        + mod("sp3", "By feature", "Where the limit went. Withdrawn work is shown separately as waste.", byFeat.length ? barList([...byFeat.map((f) => ({ l: `${avatar(project(f.project))}${f.l}`, v: f.v, sub: `${f.n} merged` })), ...(p.withdrawn.length ? [{ l: `${glyph("failed")}Withdrawn`, v: sum(p.withdrawn, (r) => r.total), sub: `${p.withdrawn.length} tickets`, tone: "is-waste" }] : [])], { fmt: pct }) : empty())
        + mod("sp4", "Merged tickets", "", p.merged.length ? capped("merged", [...p.merged].reverse(), (rows) => `<div class="cc-table-wrap"><table class="cc-table"><thead><tr><th>Ticket</th><th>Feature</th><th>Merged</th><th class="num">Agent</th><th class="num">Follow-up</th><th class="num">Total</th></tr></thead><tbody>${rows.map((r) => `<tr><td>${r.title}</td><td><span class="cc-tfeat">${avatar(project(r.project))}${r.feature}</span></td><td class="meta">${r.day}</td><td class="num">${pct(r.agent)}</td><td class="num">${r.follow ? pct(r.follow) : "—"}</td><td class="num"><b>${pct(r.total)}</b></td></tr>`).join("")}</tbody></table></div>`) : empty());
    },
    flow(p) {
      const m = p.merged;
      if (!m.length) return empty();
      const meds = SEG.map(([f, l, c]) => [l, c, median(m.map((r) => r[f]))]);
      const tot = sum(meds, (x) => x[2]);
      const scoped = (r) => (!st.iproj || r.project === st.iproj) && (!st.ifeat || r.feature === st.ifeat);
      const weeks = [3, 2, 1, 0].map((w) => {
        const inW = (r) => { const d = ago(r.day); return d >= w * 7 && d < (w + 1) * 7 && scoped(r); };
        const mw = runs.filter(inW), ww = withdrawn.filter(inW);
        const d0 = new Date(TODAY - (w + 1) * 7 * 864e5 + 864e5);
        return { l: d0.toLocaleDateString("en-US", { month: "short", day: "numeric" }), merged: mw.length, runs: sum(mw, (r) => 1 + r.rerun + r.resolve + r.followRuns) + ww.length };
      });
      const wmax = Math.max(...weeks.map((w) => w.runs), 1);
      return mod("fl1", "Where the time goes", `Median ${hm(tot)} from launch to merge. Segments are each stage's median.`,
          `<div class="ci-stack" role="img" aria-label="${meds.map(([l, , v]) => `${l} ${hm(v)}`).join(", ")}">${meds.map(([l, c, v]) => `<span class="${c}" style="flex:${v}" title="${l}: ${hm(v)}"></span>`).join("")}</div>
          <ul class="ci-stack-key">${meds.map(([l, c, v]) => `<li><i class="${c}"></i>${l}<b>${hm(v)}</b></li>`).join("")}</ul>`)
        + mod("fl2", "Per ticket", "Longest first. Indigo is time a PR sat waiting for your review.", capped("ttm", [...m].sort((a, b) => (b.runMin + b.checkMin + b.pushMin + b.reviewMin) - (a.runMin + a.checkMin + a.pushMin + a.reviewMin)), (rows) => {
          const mx = Math.max(...rows.map((r) => r.runMin + r.checkMin + r.pushMin + r.reviewMin));
          return `<ul class="ci-tl">${rows.map((r) => { const t = r.runMin + r.checkMin + r.pushMin + r.reviewMin; return `<li><span class="ci-tl-t">${r.title}<span class="meta">${r.feature}</span></span><span class="ci-tl-bar" style="--w:${(t / mx) * 100}%">${SEG.map(([f, l, c]) => `<span class="${c}" style="flex:${r[f]}" title="${l}: ${hm(r[f])}"></span>`).join("")}</span><span class="ci-bar-v">${hm(t)}</span></li>`; }).join("")}</ul>`;
        }))
        + mod("fl3", "Throughput", "Agent runs started and tickets merged per week. A run count rising faster than merges means more rework.",
          `<div class="ci-cols">${weeks.map((w) => `<div class="ci-col"><div class="ci-col-bars"><span class="ci-col-runs" style="height:${(w.runs / wmax) * 100}%" title="${w.runs} runs"></span><span class="ci-col-merged" style="height:${(w.merged / wmax) * 100}%" title="${w.merged} merged"></span></div><span class="ci-col-n">${w.runs} · <b>${w.merged}</b></span><span class="meta">${w.l}</span></div>`).join("")}</div>`,
          legend([["cc-key-sq is-out", "Runs"], ["cc-key-sq", "Merged"]]));
    },
    quality(p) {
      const m = p.merged, done = m.length + p.withdrawn.length;
      if (!done) return empty();
      const green = m.filter((r) => r.firstGreen).length, clean = m.filter((r) => !r.rerun && !r.resolve && !r.followRuns && !r.steer).length;
      const funnel = [["Finished", done], ["Opened a PR", done], ["Checks green on first push", green], ["Merged", m.length], ["Merged with no intervention", clean]];
      const all = [...m, ...p.withdrawn];
      const tools = ["bash", "edit", "read", "grep"].map((t) => { const c = sum(all, (r) => r.tools[t][0]), f = sum(all, (r) => r.tools[t][1]); return { l: t, v: c ? (f / c) * 100 : 0, sub: `${f} of ${c} calls` }; }).sort((a, b) => b.v - a.v);
      const sizes = [["XS", 0, 100e3], ["S", 100e3, 250e3], ["M", 250e3, 400e3], ["L", 400e3, 600e3], ["XL", 600e3, Infinity]].map(([l, a, b]) => ({ l, v: all.filter((r) => r.tokIn >= a && r.tokIn < b).length, tone: l === "L" || l === "XL" ? "is-warn" : "" }));
      const iv = [["Re-runs", "rerun"], ["Resolves", "resolve"], ["Steers mid-run", "steer"], ["Follow-up runs", "followRuns"], ["Kills", "kill"]].map(([l, f]) => ({ l, v: sum(m, (r) => r[f]) }));
      return mod("q1", "From launch to clean merge", `${share(clean, done)}% of finished tickets merged with no re-run, resolve, steer or follow-up.`,
          `<ol class="ci-funnel">${funnel.map(([l, v]) => `<li><span class="ci-bar-l">${l}</span><span class="ci-bar"><i style="width:${share(v, done)}%"></i></span><span class="ci-bar-v">${v}<span class="meta">${share(v, done)}%</span></span></li>`).join("")}</ol>`)
        + mod("q2", "First-push failures by check", "Which required check most often sends an agent back.", barList(CHECKS.map((c) => ({ l: c, v: m.filter((r) => r.failedCheck === c).length, sub: "tickets" })).sort((a, b) => b.v - a.v), { tone: "is-fail" }))
        + `<div class="ci-two">${mod("q3", "Interventions", "Times you stepped in.", barList(iv))}${mod("q4", "Run size", "By input tokens. L and XL runs are worth splitting.", barList(sizes))}</div>`
        + mod("q5", "Tool-call failure rate", "Failed tool calls as a share of all calls, from the run logs.", barList(tools, { fmt: (v) => `${v.toFixed(1)}%`, tone: "is-fail" }));
    },
  };

  pages.insights = () => {
    if (TABS[st.v]) { st.itab = st.v; st.v = "A"; }
    const p = period(), s = stats(p), prev = stats(period(1));
    const feats = FEATURES.filter((f) => !st.iproj || f.project === st.iproj);
    const filtered = st.iproj || st.ifeat;
    main().innerHTML = `
      ${topbar("<strong>Insights</strong>")}
      <div class="cc-page cc-page-narrow ci-page">
        <div class="cc-page-head"><h1>Insights</h1><p>What your agents cost, how fast work moves, and where it goes wrong.</p></div>
        <div class="ci-filters" role="toolbar" aria-label="Filters">
          <div class="tabs" role="radiogroup" aria-label="Range">${[7, 14, 30].map((n) => `<button class="tab" role="radio" type="button" aria-checked="${st.irange === n}" aria-selected="${st.irange === n}" data-act="irange" data-v="${n}">${n} days</button>`).join("")}</div>
          <label class="cc-select"><span class="sr-only">Project</span><select data-iproj><option value="">All projects</option>${PROJECTS.map((x) => `<option value="${x.id}" ${st.iproj === x.id ? "selected" : ""}>${x.name}</option>`).join("")}</select>${icon("down", "icon icon-sm")}</label>
          <label class="cc-select"><span class="sr-only">Feature</span><select data-ifeat><option value="">All features</option>${[...new Set([...feats.map((f) => f.title), ...runs.filter((r) => !st.iproj || r.project === st.iproj).map((r) => r.feature)])].map((f) => `<option ${st.ifeat === f ? "selected" : ""}>${f}</option>`).join("")}</select>${icon("down", "icon icon-sm")}</label>
          ${filtered ? `<button class="btn btn-ghost btn-sm" type="button" data-act="iclear">Clear filters</button>` : ""}
          <span class="ci-count">${s.merged} merged · ${p.withdrawn.length} withdrawn</span>
        </div>
        <div class="tabs ci-tabs" role="tablist" aria-label="Insight views">${[["overview", "Overview"], ["spend", "Spend"], ["flow", "Flow"], ["quality", "Quality"]].map(([id, l]) => `<button class="tab" role="tab" type="button" aria-selected="${st.itab === id}" data-act="itab" data-v="${id}">${l}</button>`).join("")}</div>
        <div role="tabpanel">${TABS[st.itab](p, s, prev)}</div>
      </div>`;
  };

  const baseAct = pages.onAct, baseChange = pages.onChange;
  pages.onAct = (a, el, e) => {
    const v = el.dataset.v;
    if (a === "irange") { st.irange = Number(v); return APP.renderMain(); }
    if (a === "itab") { st.itab = v; return APP.renderMain(); }
    if (a === "iall") { st.iall.has(v) ? st.iall.delete(v) : st.iall.add(v); return APP.renderMain(); }
    if (a === "iclear") { st.iproj = ""; st.ifeat = ""; return APP.renderMain(); }
    return baseAct && baseAct(a, el, e);
  };
  pages.onChange = (e) => {
    if (e.target.matches("[data-iproj]")) { st.iproj = e.target.value; st.ifeat = ""; return APP.renderMain(); }
    if (e.target.matches("[data-ifeat]")) { st.ifeat = e.target.value; return APP.renderMain(); }
    return baseChange && baseChange(e);
  };
})();
