import { c as X, n as Y, j as Z, a as _, o as ee, e as k, x as te, m as ne, k as re, i as o, f as h, S as y, F as I, g as v, h as x, q as le, s as M, y as C, t as m, b as ae } from "./layout.js";
const T = "now", A = "on unlock", F = "refused";
function se(l) {
  return new Set(l.filter((r) => r.label !== F).map((r) => r.url));
}
function oe(l, r, g) {
  return l.filter((i) => i.url !== g && r.has(i.url) && (i.blocked_by || []).includes(g));
}
function ie(l, r, g) {
  const i = new Map(l.map((d) => [d.url, d])), u = i.get(g);
  return ((u == null ? void 0 : u.blocked_by) || []).filter((d) => i.has(d)).filter((d) => !r.has(d)).map((d) => i.get(d));
}
function ue(l) {
  const r = new Map(l.map((u) => [u.url, u])), g = /* @__PURE__ */ new Map();
  function i(u) {
    var E;
    const p = g.get(u);
    if (p !== void 0) return p;
    g.set(u, 0);
    const d = (((E = r.get(u)) == null ? void 0 : E.blocked_by) || []).filter((B) => r.has(B)), S = d.length === 0 ? 0 : 1 + Math.max(...d.map(i));
    return g.set(u, S), S;
  }
  for (const u of l) i(u.url);
  return g;
}
function de(l) {
  return {
    now: l.filter((r) => r.label === T),
    onUnlock: l.filter((r) => r.label === A),
    refused: l.filter((r) => r.label !== T && r.label !== A)
  };
}
function H(l) {
  return l ? `/f/${encodeURIComponent(l)}` : "/";
}
var ce = /* @__PURE__ */ m('<p class="banner ribbon mb-2"data-glyph=failed role=status>'), he = /* @__PURE__ */ m("<section class=launch-section><h3 class=launch-section-title> <span class=text-muted></span></h3><ul class=launch-rows>"), fe = /* @__PURE__ */ m("<input type=hidden name=ticket>"), ge = /* @__PURE__ */ m("<input type=hidden name=hash>"), be = /* @__PURE__ */ m('<li class=launch-row><label class=launch-row-main><input type=checkbox><span class=launch-row-ref></span><span></span></label><span class="launch-row-meta text-muted"> &middot; '), me = /* @__PURE__ */ m('<div class="relative mb-3 overflow-hidden rounded border border-border"><svg aria-hidden=true role=presentation>'), ve = /* @__PURE__ */ m('<form method=post action=/launch><div class=launch-actions><a class=button>Cancel</a><button type=submit class="button launch-confirm">Confirm launch'), we = /* @__PURE__ */ m("<div>"), $e = /* @__PURE__ */ m("<p class=text-muted>loading&hellip;"), pe = /* @__PURE__ */ m('<svg><path class="fill-none stroke-border stroke-[1.5]"></svg>', !1, !0, !1), _e = /* @__PURE__ */ m('<div class="absolute flex w-[200px] items-center overflow-hidden text-ellipsis whitespace-nowrap rounded border border-border bg-bg px-2 py-[0.3rem] text-[0.85em]"> ');
function O(l) {
  const r = l.split("/");
  return `#${r[r.length - 1]}`;
}
function W(l) {
  return (
    // biome-ignore lint/a11y/useSemanticElements: <output> would break that byte-for-byte match.
    (() => {
      var r = ce();
      return o(r, () => l.children), r;
    })()
  );
}
X("cc-launch-modal", {
  feature: "",
  tickets: ""
}, (l) => {
  Y(), Z().textContent = "";
  const [r, g] = _([]), [i, u] = _(/* @__PURE__ */ new Set()), [p, d] = _(null), [S, E] = _(!1), [B, z] = _(!1);
  ee(async () => {
    const t = (n) => {
      n.key === "Escape" && window.location.assign(H(l.feature));
    };
    document.addEventListener("keydown", t), ae(() => document.removeEventListener("keydown", t));
    try {
      const n = l.tickets || `feature=${encodeURIComponent(l.feature)}`, s = await fetch(`/launch/candidates?${n}`);
      if (!s.ok) throw new Error(String(s.status));
      const e = await s.json();
      g(e), u(se(e));
    } catch {
      E(!0);
    } finally {
      z(!0);
    }
  });
  function K(t) {
    return t.map((n) => `${O(n.url)} ${n.title}`).join(", ");
  }
  function G(t) {
    if (t.label === F) return;
    if (!i().has(t.url)) {
      const s = ie(r(), i(), t.url);
      if (s.length > 0) {
        d(`${O(t.url)} is blocked by ${K(s)} -- tick it first`);
        return;
      }
      d(null), u((e) => new Set(e).add(t.url));
      return;
    }
    const n = oe(r(), i(), t.url);
    if (n.length > 0) {
      d(`${K(n)} depends on ${O(t.url)} -- clear it first`);
      return;
    }
    d(null), u((s) => {
      const e = new Set(s);
      return e.delete(t.url), e;
    });
  }
  const P = k(() => r().filter((t) => i().has(t.url))), N = k(() => {
    const t = P(), n = ue(t);
    return te(t.map((s) => ({
      ...s,
      col: n.get(s.url) ?? 0
    })));
  }), J = k(() => ne(N(), (t) => t.blocked_by)), R = k(() => re(N())), j = k(() => de(r()));
  function q(t) {
    return h(y, {
      get when() {
        return t.rows.length > 0;
      },
      get children() {
        var n = he(), s = n.firstChild, e = s.firstChild, c = e.nextSibling, f = s.nextSibling;
        return o(s, () => t.title, e), o(c, () => t.rows.length), o(f, h(I, {
          get each() {
            return t.rows;
          },
          children: (a) => (() => {
            var b = be(), $ = b.firstChild, L = $.firstChild, D = L.nextSibling, Q = D.nextSibling, U = $.nextSibling, V = U.firstChild;
            return L.addEventListener("change", () => G(a)), o($, h(y, {
              get when() {
                return C(() => !!i().has(a.url))() && a.label !== F;
              },
              get children() {
                return [(() => {
                  var w = fe();
                  return v(() => w.value = a.url), w;
                })(), (() => {
                  var w = ge();
                  return v(() => w.value = `${a.url} ${a.prompt_hash}`), w;
                })()];
              }
            }), D), o(D, () => a.ref), o(Q, () => a.title), o(U, () => a.base, V), o(U, () => a.base_verdict, null), o(U, (() => {
              var w = C(() => !!a.reason);
              return () => w() && [" · ", C(() => a.reason)];
            })(), null), v(() => L.disabled = a.label === F), v(() => L.checked = i().has(a.url)), b;
          })()
        })), v(() => x(n, "aria-label", t.title)), n;
      }
    });
  }
  return (() => {
    var t = we();
    return o(t, h(y, {
      get when() {
        return B();
      },
      get fallback() {
        return $e();
      },
      get children() {
        return h(y, {
          get when() {
            return !S();
          },
          get fallback() {
            return h(W, {
              get children() {
                return ["could not load candidates", C(() => C(() => !!l.feature)() && ` for ${l.feature}`), "."];
              }
            });
          },
          get children() {
            return [h(y, {
              get when() {
                return p();
              },
              children: (n) => h(W, {
                get children() {
                  return n();
                }
              })
            }), (() => {
              var n = me(), s = n.firstChild;
              return o(s, h(I, {
                get each() {
                  return J();
                },
                children: (e) => (() => {
                  var c = pe();
                  return v(() => x(c, "d", le(e))), c;
                })()
              })), o(n, h(I, {
                get each() {
                  return N();
                },
                children: (e) => (() => {
                  var c = _e(), f = c.firstChild;
                  return o(c, () => O(e.url), f), o(c, () => e.title || "untitled", null), v((a) => {
                    var b = `${e.x}px`, $ = `${e.y}px`;
                    return b !== a.e && M(c, "left", a.e = b), $ !== a.t && M(c, "top", a.t = $), a;
                  }, {
                    e: void 0,
                    t: void 0
                  }), c;
                })()
              }), null), v((e) => {
                var c = `${R().width}px`, f = `${R().height}px`, a = R().width, b = R().height;
                return c !== e.e && M(n, "width", e.e = c), f !== e.t && M(n, "height", e.t = f), a !== e.a && x(s, "width", e.a = a), b !== e.o && x(s, "height", e.o = b), e;
              }, {
                e: void 0,
                t: void 0,
                a: void 0,
                o: void 0
              }), n;
            })(), (() => {
              var n = ve(), s = n.firstChild, e = s.firstChild, c = e.nextSibling;
              return o(n, h(q, {
                title: "Ready now",
                get rows() {
                  return j().now;
                }
              }), s), o(n, h(q, {
                title: "On unlock",
                get rows() {
                  return j().onUnlock;
                }
              }), s), o(n, h(q, {
                title: "Refused",
                get rows() {
                  return j().refused;
                }
              }), s), v((f) => {
                var a = H(l.feature), b = P().length === 0;
                return a !== f.e && x(e, "href", f.e = a), b !== f.t && (c.disabled = f.t = b), f;
              }, {
                e: void 0,
                t: void 0
              }), n;
            })()];
          }
        });
      }
    })), t;
  })();
});
