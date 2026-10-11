import { describe, expect, test } from "bun:test";
import {
  activate,
  bindingFor,
  createKeyState,
  type Focused,
  isApplePlatform,
  type KeyInfo,
  type KeyTarget,
  type RailLink,
  resolveTarget,
  type SearchBox,
  searchKey,
  walkRail,
} from "./keys";

function press(key: string, mods: Partial<KeyInfo> = {}): KeyInfo {
  return {
    key,
    altKey: false,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    isComposing: false,
    repeat: false,
    ...mods,
  };
}

function focused(selectors: string[], isContentEditable = false): Focused {
  return { matches: (sel) => selectors.some((s) => sel.split(", ").includes(s)), isContentEditable };
}

interface Clock {
  timers: { set(fn: () => void, ms: number): () => void };
  advance(ms: number): void;
}

function fakeClock(): Clock {
  let now = 0;
  const pending = new Map<number, { at: number; fn: () => void }>();
  let id = 0;
  return {
    timers: {
      set(fn, ms) {
        const mine = id++;
        pending.set(mine, { at: now + ms, fn });
        return () => pending.delete(mine);
      },
    },
    advance(ms) {
      now += ms;
      for (const [k, t] of [...pending]) {
        if (t.at <= now) {
          pending.delete(k);
          t.fn();
        }
      }
    },
  };
}

interface Fake extends KeyTarget {
  calls: string[];
}

function fake(
  key: string,
  opts: { main?: boolean; disabled?: boolean; hidden?: boolean; field?: boolean } = {},
): Fake {
  const calls: string[] = [];
  return {
    calls,
    dataset: { key },
    disabled: opts.disabled,
    isContentEditable: false,
    matches: (sel) => opts.field === true && sel.includes("input"),
    getClientRects: () => (opts.hidden ? [] : [{}]),
    closest: (sel) => (sel === "#main" && opts.main ? {} : null),
    focus: () => calls.push("focus"),
    select: () => calls.push("select"),
    click: () => calls.push("click"),
  };
}

describe("bindingFor", () => {
  test("names a bare key in lower case", () => {
    expect(bindingFor(press("r"), false)).toBe("r");
    expect(bindingFor(press("?", { shiftKey: true }), false)).toBe("?");
    expect(bindingFor(press("Escape"), false)).toBe("escape");
  });

  test("uses Meta as mod on Apple platforms and Ctrl elsewhere", () => {
    expect(bindingFor(press("k", { metaKey: true }), true)).toBe("mod+k");
    expect(bindingFor(press("k", { ctrlKey: true }), true)).toBeNull();
    expect(bindingFor(press("k", { ctrlKey: true }), false)).toBe("mod+k");
    expect(bindingFor(press("k", { metaKey: true }), false)).toBeNull();
  });

  test("leaves every other modified key to the browser", () => {
    expect(bindingFor(press("r", { metaKey: true }), true)).toBeNull();
    expect(bindingFor(press("r", { altKey: true }), false)).toBeNull();
    expect(bindingFor(press("R", { shiftKey: true }), false)).toBeNull();
    expect(bindingFor(press("g", { repeat: true }), false)).toBeNull();
    expect(bindingFor(press("g", { isComposing: true }), false)).toBeNull();
  });
});

describe("isApplePlatform", () => {
  test("prefers userAgentData and falls back to navigator.platform", () => {
    expect(isApplePlatform({ userAgentData: { platform: "macOS" }, platform: "Linux" })).toBe(true);
    expect(isApplePlatform({ userAgentData: { platform: "Windows" }, platform: "MacIntel" })).toBe(false);
    expect(isApplePlatform({ platform: "iPhone" })).toBe(true);
    expect(isApplePlatform({ platform: "Win32" })).toBe(false);
    expect(isApplePlatform({})).toBe(false);
  });
});

describe("the g chord", () => {
  test("g then a key looks up the chord", () => {
    const state = createKeyState(false, fakeClock().timers);
    expect(state.lookup(press("g"), null)).toBeNull();
    expect(state.lookup(press("t"), null)).toBe("g t");
    expect(state.lookup(press("t"), null)).toBe("t");
  });

  test("expires after 1.5 seconds", () => {
    const clock = fakeClock();
    const state = createKeyState(false, clock.timers);
    state.lookup(press("g"), null);
    clock.advance(1499);
    expect(state.lookup(press("t"), null)).toBe("g t");

    state.lookup(press("g"), null);
    clock.advance(1500);
    expect(state.lookup(press("t"), null)).toBe("t");
  });

  test("g then a digit 0 to 9 looks up the scope chord", () => {
    const state = createKeyState(true, fakeClock().timers);
    for (let d = 0; d <= 9; d++) {
      expect(state.lookup(press("g"), null)).toBeNull();
      expect(state.lookup(press(String(d)), null)).toBe(`g ${d}`);
    }
  });

  test("the scope chord is ignored while a field has focus", () => {
    const state = createKeyState(true, fakeClock().timers);
    const input = focused(["input"]);
    expect(state.lookup(press("g"), input)).toBeNull();
    expect(state.lookup(press("1"), input)).toBeNull();
    expect(state.lookup(press("1"), null)).toBe("1");
  });

  test("a modified digit is left to the browser", () => {
    expect(bindingFor(press("1", { metaKey: true }), true)).toBeNull();
    expect(bindingFor(press("1", { ctrlKey: true }), false)).toBeNull();
  });

  test("clear drops a pending prefix", () => {
    const state = createKeyState(false, fakeClock().timers);
    state.lookup(press("g"), null);
    state.clear();
    expect(state.lookup(press("t"), null)).toBe("t");
  });

  test("a second g is consumed as the chord g g", () => {
    const state = createKeyState(false, fakeClock().timers);
    state.lookup(press("g"), null);
    expect(state.lookup(press("g"), null)).toBe("g g");
    expect(state.lookup(press("t"), null)).toBe("t");
  });
});

describe("field and Enter guards", () => {
  test("typing in a field reaches no binding", () => {
    const state = createKeyState(false, fakeClock().timers);
    const input = focused(["input"]);
    expect(state.lookup(press("g"), input)).toBeNull();
    expect(state.lookup(press("t"), input)).toBeNull();
    expect(state.lookup(press("c"), input)).toBeNull();
  });

  test("typing g in a field does not arm the chord", () => {
    const state = createKeyState(false, fakeClock().timers);
    state.lookup(press("g"), focused(["textarea"]));
    expect(state.lookup(press("t"), null)).toBe("t");
  });

  test("a contenteditable counts as a field", () => {
    const state = createKeyState(false, fakeClock().timers);
    expect(state.lookup(press("c"), focused([], true))).toBeNull();
  });

  test("escape and mod+k still work from inside a field", () => {
    const state = createKeyState(false, fakeClock().timers);
    const select = focused(["select"]);
    expect(state.lookup(press("Escape"), select)).toBe("escape");
    expect(state.lookup(press("k", { ctrlKey: true }), select)).toBe("mod+k");
  });

  test("Enter on a link, button or summary is left to the element", () => {
    const state = createKeyState(false, fakeClock().timers);
    for (const tag of ["a", "button", "summary"]) {
      expect(state.lookup(press("Enter"), focused([tag]))).toBeNull();
    }
    expect(state.lookup(press("Enter"), focused(["div"]))).toBe("enter");
    expect(state.lookup(press("Enter"), null)).toBe("enter");
  });
});

describe("resolveTarget", () => {
  test("returns null when nothing is bound", () => {
    expect(resolveTarget("c", [fake("t")])).toBeNull();
  });

  test("skips disabled and hidden elements", () => {
    const usable = fake("c");
    expect(resolveTarget("c", [fake("c", { disabled: true }), fake("c", { hidden: true }), usable])).toBe(usable);
  });

  test("an element inside #main beats one outside it, whichever comes first", () => {
    const rail = fake("c");
    const main = fake("c", { main: true });
    expect(resolveTarget("c", [rail, main])).toBe(main);
    expect(resolveTarget("c", [main, rail])).toBe(main);
  });

  test("the first of equals wins", () => {
    const first = fake("c");
    expect(resolveTarget("c", [first, fake("c")])).toBe(first);
  });

  test("matches a chord binding exactly", () => {
    const go = fake("g t");
    expect(resolveTarget("g t", [fake("t"), go])).toBe(go);
  });
});

function rail(current: number | null, count: number, folded: number[] = []) {
  const clicks: number[] = [];
  const folds = new Map<number, { open: boolean }>();
  const links: RailLink[] = Array.from({ length: count }, (_, i) => {
    const fold = { open: !folded.includes(i) };
    folds.set(i, fold);
    return {
      getAttribute: (n) => (n === "aria-current" && i === current ? "page" : null),
      closest: () => fold,
      click: () => clicks.push(i),
    };
  });
  return { links, clicks, folds };
}

describe("walkRail", () => {
  test("J and K step from the current link", () => {
    const r = rail(1, 3);
    walkRail(r.links, 1);
    walkRail(r.links, -1);
    expect(r.clicks).toEqual([2, 0]);
  });

  test("with no current link J goes first and K goes last", () => {
    const r = rail(null, 3);
    walkRail(r.links, 1);
    walkRail(r.links, -1);
    expect(r.clicks).toEqual([0, 2]);
  });

  test("does nothing past either end", () => {
    const last = rail(2, 3);
    const first = rail(0, 3);
    expect(walkRail(last.links, 1)).toBeNull();
    expect(walkRail(first.links, -1)).toBeNull();
    expect(walkRail([], 1)).toBeNull();
    expect(last.clicks).toEqual([]);
    expect(first.clicks).toEqual([]);
  });

  test("unfolds a closed details before clicking", () => {
    const r = rail(0, 2, [1]);
    walkRail(r.links, 1);
    expect(r.folds.get(1)?.open).toBe(true);
    expect(r.clicks).toEqual([1]);
  });
});

describe("activate", () => {
  test("focuses a field and clicks anything else", () => {
    const field = fake("mod+k", { field: true });
    const link = fake("g h");
    activate(field);
    activate(link);
    expect(field.calls).toEqual(["focus", "select"]);
    expect(link.calls).toEqual(["click"]);
  });
});

describe("searchKey", () => {
  function box(value: string): SearchBox & { events: string[]; blurs: number } {
    const b = {
      value,
      events: [] as string[],
      blurs: 0,
      blur: () => void b.blurs++,
      dispatchEvent: (e: Event) => {
        b.events.push(e.type);
        return true;
      },
    };
    return b;
  }

  test("Enter clicks the first match and keeps the query", () => {
    const b = box("auth");
    const clicks: string[] = [];
    const links = [{ click: () => clicks.push("a") }, { click: () => clicks.push("b") }];
    expect(searchKey("Enter", b, links)).toBe(true);
    expect(clicks).toEqual(["a"]);
    expect(b.value).toBe("auth");
    expect(b.blurs).toBe(1);
  });

  test("Escape on an empty box blurs without a search event", () => {
    const b = box("");
    searchKey("Escape", b, []);
    expect(b.events).toEqual([]);
    expect(b.blurs).toBe(1);
  });

  test("Enter with no match does nothing", () => {
    const b = box("zzz");
    expect(searchKey("Enter", b, [])).toBe(true);
    expect(b.blurs).toBe(0);
    expect(b.value).toBe("zzz");
    expect(b.events).toEqual([]);
  });

  test("Escape clears, fires search and blurs", () => {
    const b = box("auth");
    expect(searchKey("Escape", b, [])).toBe(true);
    expect(b.value).toBe("");
    expect(b.events).toEqual(["search"]);
    expect(b.blurs).toBe(1);
  });

  test("other keys are left alone", () => {
    expect(searchKey("a", box("x"), [])).toBe(false);
  });
});
