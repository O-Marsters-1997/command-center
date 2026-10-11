const ae = (e, t) => e === t, he = Symbol("solid-track"), L = {
  equals: ae
};
let Y = se;
const m = 1, R = 2, Z = {
  owned: null,
  cleanups: null,
  context: null,
  owner: null
};
var d = null;
let F = null, de = null, h = null, p = null, C = null, V = 0;
function M(e, t) {
  const n = h, r = d, s = e.length === 0, o = t === void 0 ? r : t, i = s ? Z : {
    owned: null,
    cleanups: null,
    context: o ? o.context : null,
    owner: o
  }, l = s ? e : () => e(() => _(() => $(i)));
  d = i, h = null;
  try {
    return j(l, !0);
  } finally {
    h = n, d = r;
  }
}
function ee(e, t) {
  t = t ? Object.assign({}, L, t) : L;
  const n = {
    value: e,
    observers: null,
    observerSlots: null,
    comparator: t.equals || void 0
  }, r = (s) => (typeof s == "function" && (s = s(n.value)), ne(n, s));
  return [te.bind(n), r];
}
function D(e, t, n) {
  const r = H(e, t, !1, m);
  N(r);
}
function pe(e, t, n) {
  Y = we;
  const r = H(e, t, !1, m);
  r.user = !0, C ? C.push(r) : N(r);
}
function k(e, t, n) {
  n = n ? Object.assign({}, L, n) : L;
  const r = H(e, t, !0, 0);
  return r.observers = null, r.observerSlots = null, r.comparator = n.equals || void 0, N(r), te.bind(r);
}
function _(e) {
  if (h === null) return e();
  const t = h;
  h = null;
  try {
    return e();
  } finally {
    h = t;
  }
}
function Ve(e) {
  pe(() => _(e));
}
function ge(e) {
  return d === null || (d.cleanups === null ? d.cleanups = [e] : d.cleanups.push(e)), e;
}
function te() {
  if (this.sources && this.state)
    if (this.state === m) N(this);
    else {
      const e = p;
      p = null, j(() => B(this), !1), p = e;
    }
  if (h) {
    const e = this.observers;
    if (!e || e[e.length - 1] !== h) {
      const t = e ? e.length : 0;
      h.sources ? (h.sources.push(this), h.sourceSlots.push(t)) : (h.sources = [this], h.sourceSlots = [t]), e ? (e.push(h), this.observerSlots.push(h.sources.length - 1)) : (this.observers = [h], this.observerSlots = [h.sources.length - 1]);
    }
  }
  return this.value;
}
function ne(e, t, n) {
  let r = e.value;
  return (!e.comparator || !e.comparator(r, t)) && (e.value = t, e.observers && e.observers.length && j(() => {
    for (let s = 0; s < e.observers.length; s += 1) {
      const o = e.observers[s], i = F && F.running;
      i && F.disposed.has(o), (i ? !o.tState : !o.state) && (o.pure ? p.push(o) : C.push(o), o.observers && re(o)), i || (o.state = m);
    }
    if (p.length > 1e6)
      throw p = [], new Error();
  }, !1)), t;
}
function N(e) {
  if (!e.fn) return;
  $(e);
  const t = V;
  ye(e, e.value, t);
}
function ye(e, t, n) {
  let r;
  const s = d, o = h;
  h = d = e;
  try {
    r = e.fn(t);
  } catch (i) {
    return e.pure && (e.state = m, e.owned && e.owned.forEach($), e.owned = null), e.updatedAt = n + 1, oe(i);
  } finally {
    h = o, d = s;
  }
  (!e.updatedAt || e.updatedAt <= n) && (e.updatedAt != null && "observers" in e ? ne(e, r) : e.value = r, e.updatedAt = n);
}
function H(e, t, n, r = m, s) {
  const o = {
    fn: e,
    state: r,
    updatedAt: null,
    owned: null,
    sources: null,
    sourceSlots: null,
    cleanups: null,
    value: t,
    owner: d,
    context: d ? d.context : null,
    pure: n
  };
  return d === null || d !== Z && (d.owned ? d.owned.push(o) : d.owned = [o]), o;
}
function K(e) {
  if (e.state === 0) return;
  if (e.state === R) return B(e);
  if (e.suspense && _(e.suspense.inFallback)) return e.suspense.effects.push(e);
  const t = [e];
  for (; (e = e.owner) && (!e.updatedAt || e.updatedAt < V); )
    e.state && t.push(e);
  for (let n = t.length - 1; n >= 0; n--)
    if (e = t[n], e.state === m)
      N(e);
    else if (e.state === R) {
      const r = p;
      p = null, j(() => B(e, t[0]), !1), p = r;
    }
}
function j(e, t) {
  if (p) return e();
  let n = !1;
  t || (p = []), C ? n = !0 : C = [], V++;
  try {
    const r = e();
    return be(n), r;
  } catch (r) {
    n || (C = null), p = null, oe(r);
  }
}
function be(e) {
  if (p && (se(p), p = null), e) return;
  const t = C;
  C = null, t.length && j(() => Y(t), !1);
}
function se(e) {
  for (let t = 0; t < e.length; t++) K(e[t]);
}
function we(e) {
  let t, n = 0;
  for (t = 0; t < e.length; t++) {
    const r = e[t];
    r.user ? e[n++] = r : K(r);
  }
  for (t = 0; t < n; t++) K(e[t]);
}
function B(e, t) {
  e.state = 0;
  for (let n = 0; n < e.sources.length; n += 1) {
    const r = e.sources[n];
    if (r.sources) {
      const s = r.state;
      s === m ? r !== t && (!r.updatedAt || r.updatedAt < V) && K(r) : s === R && B(r, t);
    }
  }
}
function re(e) {
  for (let t = 0; t < e.observers.length; t += 1) {
    const n = e.observers[t];
    n.state || (n.state = R, n.pure ? p.push(n) : C.push(n), n.observers && re(n));
  }
}
function $(e) {
  let t;
  if (e.sources)
    for (; e.sources.length; ) {
      const n = e.sources.pop(), r = e.sourceSlots.pop(), s = n.observers;
      if (s && s.length) {
        const o = s.pop(), i = n.observerSlots.pop();
        r < s.length && (o.sourceSlots[i] = r, s[r] = o, n.observerSlots[r] = i);
      }
    }
  if (e.tOwned) {
    for (t = e.tOwned.length - 1; t >= 0; t--) $(e.tOwned[t]);
    delete e.tOwned;
  }
  if (e.owned) {
    for (t = e.owned.length - 1; t >= 0; t--) $(e.owned[t]);
    e.owned = null;
  }
  if (e.cleanups) {
    for (t = e.cleanups.length - 1; t >= 0; t--) e.cleanups[t]();
    e.cleanups = null;
  }
  e.state = 0;
}
function Ce(e) {
  return e instanceof Error ? e : new Error(typeof e == "string" ? e : "Unknown error", {
    cause: e
  });
}
function oe(e, t = d) {
  throw Ce(e);
}
const _e = Symbol("fallback");
function G(e) {
  for (let t = 0; t < e.length; t++) e[t]();
}
function me(e, t, n = {}) {
  let r = [], s = [], o = [], i = 0, l = t.length > 1 ? [] : null;
  return ge(() => G(o)), () => {
    let u = e() || [], f = u.length, a, c;
    return u[he], _(() => {
      let g, A, S, P, T, y, b, w, x;
      if (f === 0)
        i !== 0 && (G(o), o = [], r = [], s = [], i = 0, l && (l = [])), n.fallback && (r = [_e], s[0] = M((fe) => (o[0] = fe, n.fallback())), i = 1);
      else if (i === 0) {
        for (s = new Array(f), c = 0; c < f; c++)
          r[c] = u[c], s[c] = M(E);
        i = f;
      } else {
        for (S = new Array(f), P = new Array(f), l && (T = new Array(f)), y = 0, b = Math.min(i, f); y < b && r[y] === u[y]; y++) ;
        for (b = i - 1, w = f - 1; b >= y && w >= y && r[b] === u[w]; b--, w--)
          S[w] = s[b], P[w] = o[b], l && (T[w] = l[b]);
        for (g = /* @__PURE__ */ new Map(), A = new Array(w + 1), c = w; c >= y; c--)
          x = u[c], a = g.get(x), A[c] = a === void 0 ? -1 : a, g.set(x, c);
        for (a = y; a <= b; a++)
          x = r[a], c = g.get(x), c !== void 0 && c !== -1 ? (S[c] = s[a], P[c] = o[a], l && (T[c] = l[a]), c = A[c], g.set(x, c)) : o[a]();
        for (c = y; c < f; c++)
          c in S ? (s[c] = S[c], o[c] = P[c], l && (l[c] = T[c], l[c](c))) : s[c] = M(E);
        s = s.slice(0, i = f), r = u.slice(0);
      }
      return s;
    });
    function E(g) {
      if (o[c] = g, l) {
        const [A, S] = ee(c);
        return l[c] = S, t(u[c], A);
      }
      return t(u[c]);
    }
  };
}
function Fe(e, t) {
  return _(() => e(t || {}));
}
const Ae = (e) => `Stale read from <${e}>.`;
function De(e) {
  const t = "fallback" in e && {
    fallback: () => e.fallback
  };
  return k(me(() => e.each, e.children, t || void 0));
}
function We(e) {
  const t = e.keyed, n = k(() => e.when, void 0, void 0), r = t ? n : k(n, void 0, {
    equals: (s, o) => !s == !o
  });
  return k(() => {
    const s = r();
    if (s) {
      const o = e.children;
      return typeof o == "function" && o.length > 0 ? _(() => o(t ? s : () => {
        if (!_(r)) throw Ae("Show");
        return n();
      })) : o;
    }
    return e.fallback;
  }, void 0, void 0);
}
const qe = (e) => k(() => e());
function Se(e, t, n) {
  let r = n.length, s = t.length, o = r, i = 0, l = 0, u = t[s - 1].nextSibling, f = null;
  for (; i < s || l < o; ) {
    if (t[i] === n[l]) {
      i++, l++;
      continue;
    }
    for (; t[s - 1] === n[o - 1]; )
      s--, o--;
    if (s === i) {
      const a = o < r ? l ? n[l - 1].nextSibling : n[o - l] : u;
      for (; l < o; ) e.insertBefore(n[l++], a);
    } else if (o === l)
      for (; i < s; )
        (!f || !f.has(t[i])) && t[i].remove(), i++;
    else if (t[i] === n[o - 1] && n[l] === t[s - 1]) {
      const a = t[--s].nextSibling;
      e.insertBefore(n[l++], t[i++].nextSibling), e.insertBefore(n[--o], a), t[s] = n[o];
    } else {
      if (!f) {
        f = /* @__PURE__ */ new Map();
        let c = l;
        for (; c < o; ) f.set(n[c], c++);
      }
      const a = f.get(t[i]);
      if (a != null)
        if (l < a && a < o) {
          let c = i, E = 1, g;
          for (; ++c < s && c < o && !((g = f.get(t[c])) == null || g !== a + E); )
            E++;
          if (E > a - l) {
            const A = t[i];
            for (; l < a; ) e.insertBefore(n[l++], A);
          } else e.replaceChild(n[l++], t[i++]);
        } else i++;
      else t[i++].remove();
    }
  }
}
const z = "_$DX_DELEGATE";
function He(e, t, n, r) {
  let s;
  const o = () => {
    const l = r ? document.createElementNS("http://www.w3.org/1998/Math/MathML", "template") : document.createElement("template");
    return l.innerHTML = e, n ? l.content.firstChild.firstChild : r ? l.firstChild : l.content.firstChild;
  }, i = t ? () => _(() => document.importNode(s || (s = o()), !0)) : () => (s || (s = o())).cloneNode(!0);
  return i.cloneNode = i, i;
}
function Ge(e, t = window.document) {
  const n = t[z] || (t[z] = /* @__PURE__ */ new Set());
  for (let r = 0, s = e.length; r < s; r++) {
    const o = e[r];
    n.has(o) || (n.add(o), t.addEventListener(o, xe));
  }
}
function ze(e, t, n) {
  n == null ? e.removeAttribute(t) : e.setAttribute(t, n);
}
function Je(e, t) {
  t == null ? e.removeAttribute("class") : e.className = t;
}
function Qe(e, t, n = {}) {
  const r = Object.keys(t || {}), s = Object.keys(n);
  let o, i;
  for (o = 0, i = s.length; o < i; o++) {
    const l = s[o];
    !l || l === "undefined" || t[l] || (J(e, l, !1), delete n[l]);
  }
  for (o = 0, i = r.length; o < i; o++) {
    const l = r[o], u = !!t[l];
    !l || l === "undefined" || n[l] === u || !u || (J(e, l, !0), n[l] = u);
  }
  return n;
}
function Xe(e, t, n) {
  n != null ? e.style.setProperty(t, n) : e.style.removeProperty(t);
}
function Ye(e, t, n) {
  return _(() => e(t, n));
}
function Ee(e, t, n, r) {
  if (n !== void 0 && !r && (r = []), typeof t != "function") return U(e, t, r, n);
  D((s) => U(e, t(), s, n), r);
}
function J(e, t, n) {
  const r = t.trim().split(/\s+/);
  for (let s = 0, o = r.length; s < o; s++) e.classList.toggle(r[s], n);
}
function xe(e) {
  let t = e.target;
  const n = `$$${e.type}`, r = e.target, s = e.currentTarget, o = (u) => Object.defineProperty(e, "target", {
    configurable: !0,
    value: u
  }), i = () => {
    const u = t[n];
    if (u && !t.disabled) {
      const f = t[`${n}Data`];
      if (f !== void 0 ? u.call(t, f, e) : u.call(t, e), e.cancelBubble) return;
    }
    return t.host && typeof t.host != "string" && !t.host._$host && t.contains(e.target) && o(t.host), !0;
  }, l = () => {
    for (; i() && (t = t._$host || t.parentNode || t.host); ) ;
  };
  if (Object.defineProperty(e, "currentTarget", {
    configurable: !0,
    get() {
      return t || document;
    }
  }), e.composedPath) {
    const u = e.composedPath();
    o(u[0]);
    for (let f = 0; f < u.length - 2 && (t = u[f], !!i()); f++) {
      if (t._$host) {
        t = t._$host, l();
        break;
      }
      if (t.parentNode === s)
        break;
    }
  } else l();
  o(r);
}
function U(e, t, n, r, s) {
  for (; typeof n == "function"; ) n = n();
  if (t === n) return n;
  const o = typeof t, i = r !== void 0;
  if (e = i && n[0] && n[0].parentNode || e, o === "string" || o === "number") {
    if (o === "number" && (t = t.toString(), t === n))
      return n;
    if (i) {
      let l = n[0];
      l && l.nodeType === 3 ? l.data !== t && (l.data = t) : l = document.createTextNode(t), n = v(e, n, r, l);
    } else
      n !== "" && typeof n == "string" ? n = e.firstChild.data = t : n = e.textContent = t;
  } else if (t == null || o === "boolean")
    n = v(e, n, r);
  else {
    if (o === "function")
      return D(() => {
        let l = t();
        for (; typeof l == "function"; ) l = l();
        n = U(e, l, n, r);
      }), () => n;
    if (Array.isArray(t)) {
      const l = [], u = n && Array.isArray(n);
      if (W(l, t, n, s))
        return D(() => n = U(e, l, n, r, !0)), () => n;
      if (l.length === 0) {
        if (n = v(e, n, r), i) return n;
      } else u ? n.length === 0 ? Q(e, l, r) : Se(e, n, l) : (n && v(e), Q(e, l));
      n = l;
    } else if (t.nodeType) {
      if (Array.isArray(n)) {
        if (i) return n = v(e, n, r, t);
        v(e, n, null, t);
      } else n == null || n === "" || !e.firstChild ? e.appendChild(t) : e.replaceChild(t, e.firstChild);
      n = t;
    }
  }
  return n;
}
function W(e, t, n, r) {
  let s = !1;
  for (let o = 0, i = t.length; o < i; o++) {
    let l = t[o], u = n && n[e.length], f;
    if (!(l == null || l === !0 || l === !1)) if ((f = typeof l) == "object" && l.nodeType)
      e.push(l);
    else if (Array.isArray(l))
      s = W(e, l, u) || s;
    else if (f === "function")
      if (r) {
        for (; typeof l == "function"; ) l = l();
        s = W(e, Array.isArray(l) ? l : [l], Array.isArray(u) ? u : [u]) || s;
      } else
        e.push(l), s = !0;
    else {
      const a = String(l);
      u && u.nodeType === 3 && u.data === a ? e.push(u) : e.push(document.createTextNode(a));
    }
  }
  return s;
}
function Q(e, t, n = null) {
  for (let r = 0, s = t.length; r < s; r++) e.insertBefore(t[r], n);
}
function v(e, t, n, r) {
  if (n === void 0) return e.textContent = "";
  const s = r || document.createTextNode("");
  if (t.length) {
    let o = !1;
    for (let i = t.length - 1; i >= 0; i--) {
      const l = t[i];
      if (s !== l) {
        const u = l.parentNode === e;
        !o && !i ? u ? e.replaceChild(s, l) : e.insertBefore(s, n) : u && l.remove();
      } else o = !0;
    }
  } else e.insertBefore(s, n);
  return [s];
}
function ve(e) {
  return Object.keys(e).reduce((n, r) => {
    const s = e[r];
    return n[r] = Object.assign({}, s), ie(s.value) && !je(s.value) && !Array.isArray(s.value) && (n[r].value = Object.assign({}, s.value)), Array.isArray(s.value) && (n[r].value = s.value.slice(0)), n;
  }, {});
}
function Oe(e) {
  return e ? Object.keys(e).reduce((n, r) => {
    const s = e[r];
    return n[r] = ie(s) && "value" in s ? s : {
      value: s
    }, n[r].attribute || (n[r].attribute = Ne(r)), n[r].parse = "parse" in n[r] ? n[r].parse : typeof n[r].value != "string", n;
  }, {}) : {};
}
function ke(e) {
  return Object.keys(e).reduce((n, r) => (n[r] = e[r].value, n), {});
}
function $e(e, t) {
  const n = ve(t);
  return Object.keys(t).forEach((s) => {
    const o = n[s], i = e.getAttribute(o.attribute), l = e[s];
    i != null && (o.value = o.parse ? le(i) : i), l != null && (o.value = Array.isArray(l) ? l.slice(0) : l), o.reflect && X(e, o.attribute, o.value, !!o.parse), Object.defineProperty(e, s, {
      get() {
        return o.value;
      },
      set(u) {
        const f = o.value;
        o.value = u, o.reflect && X(this, o.attribute, o.value, !!o.parse);
        for (let a = 0, c = this.__propertyChangedCallbacks.length; a < c; a++)
          this.__propertyChangedCallbacks[a](s, u, f);
      },
      enumerable: !0,
      configurable: !0
    });
  }), n;
}
function le(e) {
  if (e)
    try {
      return JSON.parse(e);
    } catch {
      return e;
    }
}
function X(e, t, n, r) {
  if (n == null || n === !1) return e.removeAttribute(t);
  let s = r ? JSON.stringify(n) : n;
  e.__updating[t] = !0, s === "true" && (s = ""), e.setAttribute(t, s), Promise.resolve().then(() => delete e.__updating[t]);
}
function Ne(e) {
  return e.replace(/\.?([A-Z]+)/g, (t, n) => "-" + n.toLowerCase()).replace("_", "-").replace(/^-/, "");
}
function ie(e) {
  return e != null && (typeof e == "object" || typeof e == "function");
}
function je(e) {
  return Object.prototype.toString.call(e) === "[object Function]";
}
function Pe(e) {
  return typeof e == "function" && e.toString().indexOf("class") === 0;
}
let O;
function Ze() {
  return O;
}
function et() {
  Object.defineProperty(O, "renderRoot", {
    value: O
  });
}
function Te(e, t) {
  const n = Object.keys(t);
  return class extends e {
    static get observedAttributes() {
      return n.map((s) => t[s].attribute);
    }
    constructor() {
      super(), this.__initialized = !1, this.__released = !1, this.__releaseCallbacks = [], this.__propertyChangedCallbacks = [], this.__updating = {}, this.props = {};
      for (let s of n)
        this[s] = void 0;
    }
    connectedCallback() {
      if (this.__initialized) return;
      this.__releaseCallbacks = [], this.__propertyChangedCallbacks = [], this.__updating = {}, this.props = $e(this, t);
      const s = ke(this.props), o = this.Component, i = O;
      try {
        O = this, this.__initialized = !0, Pe(o) ? new o(s, {
          element: this
        }) : o(s, {
          element: this
        });
      } finally {
        O = i;
      }
    }
    async disconnectedCallback() {
      if (await Promise.resolve(), this.isConnected) return;
      this.__propertyChangedCallbacks.length = 0;
      let s = null;
      for (; s = this.__releaseCallbacks.pop(); ) s(this);
      delete this.__initialized, this.__released = !0;
    }
    attributeChangedCallback(s, o, i) {
      if (this.__initialized && !this.__updating[s] && (s = this.lookupProp(s), s in t)) {
        if (i == null && !this[s]) return;
        this[s] = t[s].parse ? le(i) : i;
      }
    }
    lookupProp(s) {
      if (t)
        return n.find((o) => s === o || s === t[o].attribute);
    }
    get renderRoot() {
      return this.shadowRoot || this.attachShadow({
        mode: "open"
      });
    }
    addReleaseCallback(s) {
      this.__releaseCallbacks.push(s);
    }
    addPropertyChangedCallback(s) {
      this.__propertyChangedCallbacks.push(s);
    }
  };
}
function Me(e, t = {}, n = {}) {
  const {
    BaseElement: r = HTMLElement,
    extension: s,
    customElements: o = window.customElements
  } = n;
  return (i) => {
    if (!e) throw new Error("tag is required to register a Component");
    let l = o.get(e);
    return l ? (l.prototype.Component = i, l) : (l = Te(r, Oe(t)), l.prototype.Component = i, l.prototype.registeredTag = e, o.define(e, l, s), l);
  };
}
function Le(e) {
  const t = Object.keys(e), n = {};
  for (let r = 0; r < t.length; r++) {
    const [s, o] = ee(e[t[r]]);
    Object.defineProperty(n, t[r], {
      get: s,
      set(i) {
        o(() => i);
      }
    });
  }
  return n;
}
function Re(e) {
  if (e.assignedSlot && e.assignedSlot._$owner)
    return e.assignedSlot._$owner;
  let t = e.parentNode;
  for (; t; ) {
    if (t._$owner)
      return t._$owner;
    if (t.assignedSlot && t.assignedSlot._$owner)
      return t.assignedSlot._$owner;
    t = t.parentNode;
  }
  return e._$owner;
}
function Ke(e) {
  return (t, n) => {
    const { element: r } = n;
    return M((s) => {
      const o = Le(t);
      r.addPropertyChangedCallback((l, u) => o[l] = u), r.addReleaseCallback(() => {
        r.renderRoot.textContent = "", s();
      });
      const i = e(o, n);
      return Ee(r.renderRoot, i);
    }, Re(r));
  };
}
function nt(e, t, n) {
  return arguments.length === 2 && (n = t, t = {}), Me(e, t)(Ke(n));
}
const ue = 260, Be = 64, ce = 200, q = 40, I = 24;
function Ue(e) {
  const t = new Map(e.map((o) => [o.url, o])), n = /* @__PURE__ */ new Map(), r = /* @__PURE__ */ new Set(), s = (o) => {
    var u;
    const i = n.get(o);
    if (i !== void 0) return i;
    if (r.has(o)) return 0;
    r.add(o);
    let l = 0;
    for (const f of ((u = t.get(o)) == null ? void 0 : u.blocking) ?? [])
      t.has(f) && (l = Math.max(l, s(f) + 1));
    return r.delete(o), n.set(o, l), l;
  };
  for (const o of e) s(o.url);
  return n;
}
function st(e) {
  const t = Ue(e);
  return Ie(e.map((n) => ({ ...n, col: t.get(n.url) ?? 0 })));
}
function rt(e) {
  return `Wave ${e + 1}`;
}
function Ie(e) {
  const t = /* @__PURE__ */ new Map();
  return e.map((n) => {
    const r = t.get(n.col) ?? I;
    return t.set(n.col, r + Be), { ...n, x: I + n.col * ue, y: r };
  });
}
function ot(e, t) {
  const n = new Map(e.map((s) => [s.url, s])), r = [];
  for (const s of e)
    for (const o of t(s) || []) {
      const i = n.get(o);
      i && r.push({ from: i, to: s });
    }
  return r;
}
function lt(e) {
  const t = e.from.x + ce, n = e.from.y + q / 2, r = e.to.x, s = e.to.y + q / 2, o = Math.max(40, (r - t) / 2);
  return `M ${t} ${n} C ${t + o} ${n}, ${r - o} ${s}, ${r} ${s}`;
}
function it(e) {
  const t = e.reduce((r, s) => Math.max(r, s.col), 0), n = e.reduce((r, s) => Math.max(r, s.y), 0);
  return { width: I * 2 + t * ue + ce, height: n + q + I };
}
function ut(e, t) {
  const n = /* @__PURE__ */ new Map(), r = /* @__PURE__ */ new Map();
  for (const o of t)
    n.set(o.to.url, [...n.get(o.to.url) ?? [], o.from.url]), r.set(o.from.url, [...r.get(o.from.url) ?? [], o.to.url]);
  const s = /* @__PURE__ */ new Set([e]);
  for (const o of [n, r]) {
    const i = [e], l = /* @__PURE__ */ new Set([e]);
    for (; i.length > 0; )
      for (const u of o.get(i.pop()) ?? [])
        l.has(u) || (l.add(u), s.add(u), i.push(u));
  }
  return s;
}
export {
  ue as C,
  De as F,
  I as M,
  We as S,
  ee as a,
  ge as b,
  nt as c,
  Ge as d,
  k as e,
  Fe as f,
  D as g,
  ze as h,
  Ee as i,
  Ze as j,
  it as k,
  st as l,
  ot as m,
  et as n,
  Ve as o,
  ut as p,
  lt as q,
  Qe as r,
  Xe as s,
  He as t,
  Ye as u,
  Je as v,
  rt as w,
  Ie as x,
  qe as y
};
