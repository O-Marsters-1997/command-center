import { c as re, d as ne, n as ae, a as p, o as le, b as oe, e as b, i as v, f as E, g as A, s as x, h as k, j as ie, t as L, k as ce, l as de, m as ue, p as he, M as ve, C as fe, w as ge, F as N, q as me, r as we, u as pe, v as be } from "./layout.js";
var xe = /* @__PURE__ */ L('<div><div class="mb-[0.4rem] flex items-center gap-[0.6rem] text-[0.85em] text-muted"><span> selected</span><button type=button class=ml-auto>launch selected</button></div><div class="relative h-[70vh] overflow-auto rounded border border-border"data-graph-panel><div class=relative><svg aria-hidden=true role=presentation>'), ye = /* @__PURE__ */ L('<span class="absolute top-1 text-[0.75em] uppercase tracking-wide text-muted">'), $e = /* @__PURE__ */ L("<svg><path class=fill-none></svg>", !1, !0, !1), Ce = /* @__PURE__ */ L('<button type=button data-node class="absolute flex w-[200px] cursor-pointer items-center gap-[0.4rem] rounded border bg-bg px-2 py-[0.3rem] text-left [font:inherit] text-inherit"><span></span><span class="overflow-hidden text-ellipsis whitespace-nowrap text-[0.85em]"> ');
const ke = 5e3, Le = "(max-width: 759px)", U = /* @__PURE__ */ new Map();
function Me(d, f, M, S) {
  const u = U.get(d.url) ?? {};
  return Object.assign(u, d, {
    col: f,
    x: M,
    y: S
  }), U.set(d.url, u), u;
}
function Se(d) {
  const f = d.split("/");
  return `#${f[f.length - 1]}`;
}
re("cc-graph", {}, () => {
  ae();
  const d = ie(), f = d.dataset.src ?? `/graph.json${window.location.search}`;
  d.textContent = "";
  const [M, S] = p([]), [u, B] = p(/* @__PURE__ */ new Set()), [J, O] = p(null), [Q, P] = p(null), [V, j] = p(null), D = /* @__PURE__ */ new Map();
  async function R() {
    try {
      const t = await fetch(f);
      if (!t.ok) return;
      S(await t.json());
    } catch {
    }
  }
  let F;
  le(() => {
    R(), F = setInterval(R, ke);
  }), oe(() => clearInterval(F));
  const g = b(() => {
    const t = M().flatMap((s) => s.root ? [s.root, ...s.children] : s.children);
    return de(t).map((s) => Me(s, s.col, s.x, s.y));
  }), T = b(() => ue(g(), (t) => t.blocking)), y = b(() => ce(g())), X = b(() => g().reduce((t, s) => Math.max(t, s.col + 1), 0)), _ = b(() => {
    const t = Q() ?? J() ?? V();
    return t ? he(t, T()) : /* @__PURE__ */ new Set();
  });
  function Y(t) {
    B((s) => {
      const o = new Set(s);
      return o.has(t) ? o.delete(t) : o.add(t), o;
    });
  }
  function Z() {
    var t;
    (t = window.htmx) == null || t.ajax("POST", "/launch/open", {
      target: "#launch-modal",
      swap: "innerHTML",
      values: {
        ticket: [...u()]
      }
    });
  }
  function ee(t) {
    Y(t), window.matchMedia(Le).matches ? j((s) => s === t ? null : t) : j(null);
  }
  function te(t, s) {
    var C;
    const o = {
      ArrowUp: [0, -1],
      ArrowDown: [0, 1],
      ArrowLeft: [-1, 0],
      ArrowRight: [1, 0]
    }[t.key];
    if (!o) return;
    t.preventDefault();
    const [$] = o, m = $ === 0 ? g().filter((l) => l.col === s.col && l.url !== s.url) : g().filter((l) => Math.sign(l.col - s.col) === $);
    m.length !== 0 && (m.sort((l, w) => Math.abs(l.y - s.y) - Math.abs(w.y - s.y)), (C = D.get(m[0].url)) == null || C.focus());
  }
  return (() => {
    var t = xe(), s = t.firstChild, o = s.firstChild, $ = o.firstChild, m = o.nextSibling, C = s.nextSibling, l = C.firstChild, w = l.firstChild;
    return v(o, () => u().size, $), m.$$click = Z, v(l, E(N, {
      get each() {
        return Array.from({
          length: X()
        }, (e, a) => a);
      },
      children: (e) => (() => {
        var a = ye();
        return x(a, "left", `${ve + e * fe}px`), v(a, () => ge(e)), a;
      })()
    }), w), v(w, E(N, {
      get each() {
        return T();
      },
      children: (e) => {
        const a = () => _().has(e.from.url) && _().has(e.to.url);
        return (() => {
          var i = $e();
          return A((r) => {
            var h = {
              "stroke-border stroke-[1.5]": !a(),
              "stroke-s-live stroke-[2.5]": a()
            }, c = me(e);
            return r.e = we(i, h, r.e), c !== r.t && k(i, "d", r.t = c), r;
          }, {
            e: void 0,
            t: void 0
          }), i;
        })();
      }
    })), v(l, E(N, {
      get each() {
        return g();
      },
      children: (e) => {
        const a = () => u().has(e.url), i = () => a() || _().has(e.url);
        return (() => {
          var r = Ce(), h = r.firstChild, c = h.nextSibling, se = c.firstChild;
          return r.$$keydown = (n) => te(n, e), r.addEventListener("blur", () => O(null)), r.addEventListener("focus", (n) => n.currentTarget.matches(":focus-visible") && O(e.url)), r.addEventListener("pointerleave", () => P(null)), r.addEventListener("pointerenter", (n) => n.pointerType === "mouse" && P(e.url)), r.$$click = () => ee(e.url), pe((n) => D.set(e.url, n), r), v(h, () => e.state), v(c, () => Se(e.url), se), v(c, () => e.title || "untitled", null), A((n) => {
            var H = !i(), I = !!i(), z = !!a(), G = `${e.x}px`, W = `${e.y}px`, q = a(), K = `glyph glyph-${e.glyph}${e.alive ? " glyph-pulse" : ""}`;
            return H !== n.e && r.classList.toggle("border-border", n.e = H), I !== n.t && r.classList.toggle("border-s-live", n.t = I), z !== n.a && r.classList.toggle("shadow-[0_0_0_1px_var(--color-s-live)]", n.a = z), G !== n.o && x(r, "left", n.o = G), W !== n.i && x(r, "top", n.i = W), q !== n.n && k(r, "aria-pressed", n.n = q), K !== n.s && be(h, n.s = K), n;
          }, {
            e: void 0,
            t: void 0,
            a: void 0,
            o: void 0,
            i: void 0,
            n: void 0,
            s: void 0
          }), r;
        })();
      }
    }), null), A((e) => {
      var a = u().size === 0, i = `${y().width}px`, r = `${y().height}px`, h = y().width, c = y().height;
      return a !== e.e && (m.disabled = e.e = a), i !== e.t && x(l, "width", e.t = i), r !== e.a && x(l, "height", e.a = r), h !== e.o && k(w, "width", e.o = h), c !== e.i && k(w, "height", e.i = c), e;
    }, {
      e: void 0,
      t: void 0,
      a: void 0,
      o: void 0,
      i: void 0
    }), t;
  })();
});
ne(["click", "keydown"]);
