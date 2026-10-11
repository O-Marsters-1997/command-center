const s = /^(mac|iphone|ipad|ios)/i, a = "input, textarea, select", f = "a, button, summary";
function m(t) {
  var e;
  return s.test(((e = t.userAgentData) == null ? void 0 : e.platform) ?? t.platform ?? "");
}
function c(t) {
  return t !== null && (t.matches(a) || t.isContentEditable === !0);
}
function d(t, e) {
  if (t.altKey || t.isComposing || t.repeat) return null;
  const n = t.key.toLowerCase();
  return t.shiftKey && /^[a-z]$/.test(n) || (e ? t.ctrlKey : t.metaKey) ? null : (e ? t.metaKey : t.ctrlKey) ? n === "k" ? "mod+k" : null : n;
}
function p(t) {
  let e = null;
  function n() {
    e == null || e(), e = null;
  }
  function r(i) {
    return e ? (n(), `g ${i}`) : i === "g" ? (e = t.set(n, 1500), null) : i;
  }
  return { next: r, clear: n };
}
function y(t, e) {
  const n = p(e);
  function r(i, l) {
    const o = d(i, t);
    return o === null || c(l) && o !== "mod+k" && o !== "escape" || o === "enter" && (l != null && l.matches(f)) ? null : n.next(o);
  }
  return { lookup: r, clear: n.clear };
}
function g(t) {
  return t.disabled !== !0 && t.getClientRects().length > 0;
}
function h(t, e) {
  let n = null;
  for (const r of e)
    r.dataset.key !== t || !g(r) || (!n || r.closest("#main") && !n.closest("#main")) && (n = r);
  return n;
}
function k(t) {
  t.matches(a) ? t.focus() : t.click();
}
function b(t) {
  return t instanceof HTMLElement;
}
function v(t = document, e = navigator) {
  const n = new AbortController(), { signal: r } = n, i = y(m(e), {
    set: (l, o) => {
      const u = setTimeout(l, o);
      return () => clearTimeout(u);
    }
  });
  return t.addEventListener(
    "keydown",
    (l) => {
      const o = i.lookup(l, t.activeElement);
      if (o === null) return;
      const u = h(o, [...t.querySelectorAll("[data-key]")].filter(b));
      u && (l.preventDefault(), k(u));
    },
    { signal: r }
  ), t.addEventListener(
    "focusin",
    (l) => {
      l.target instanceof Element && c(l.target) && i.clear();
    },
    { signal: r }
  ), () => {
    n.abort(), i.clear();
  };
}
typeof document < "u" && v();
export {
  k as activate,
  d as bindingFor,
  p as createChord,
  y as createKeyState,
  v as initKeys,
  m as isApplePlatform,
  c as isField,
  h as resolveTarget
};
