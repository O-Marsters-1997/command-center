import { describe, expect, test } from "bun:test";

declare module "bun:test" {
  namespace test {
    function each<const Row extends readonly unknown[]>(cases: readonly Row[]):
      (name: string, run: (...args: [...Row]) => void | Promise<void>) => void;
    function each<const Value extends string>(cases: readonly Value[]):
      (name: string, run: (value: Value) => void | Promise<void>) => void;
  }
}
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
  opts: { main?: boolean; disabled?: boolean; hidden?: boolean; field?: boolean; menu?: { open: boolean } } = {},
): Fake {
  const calls: string[] = [];
  return {
    calls,
    dataset: { key },
    disabled: opts.disabled,
    isContentEditable: false,
    matches: (sel) => opts.field === true && sel.includes("input"),
    getClientRects: () => (opts.hidden ? [] : [{}]),
    closest: (sel) => {
      if (sel === "#main") return opts.main ? {} : null;
      return sel.includes("data-scope-menu") ? (opts.menu ?? null) : null;
    },
    focus: () => calls.push("focus"),
    select: () => calls.push("select"),
    click: () => calls.push("click"),
  };
}

describe("bindingFor", () => {
  test.each([
    ["lowercase letter", press("r"), false, "r"],
    ["shifted punctuation", press("?", { shiftKey: true }), false, "?"],
    ["lowercase Escape", press("Escape"), false, "escape"],
    ["Apple Meta+K", press("k", { metaKey: true }), true, "mod+k"],
    ["Apple Ctrl+K", press("k", { ctrlKey: true }), true, null],
    ["other Ctrl+K", press("k", { ctrlKey: true }), false, "mod+k"],
    ["other Meta+K", press("k", { metaKey: true }), false, null],
    ["modified letter", press("r", { metaKey: true }), true, null],
    ["Alt key", press("r", { altKey: true }), false, null],
    ["shifted letter", press("R", { shiftKey: true }), false, null],
    ["repeat", press("g", { repeat: true }), false, null],
    ["composition", press("g", { isComposing: true }), false, null],
    ["Apple modified digit", press("1", { metaKey: true }), true, null],
    ["other modified digit", press("1", { ctrlKey: true }), false, null],
  ] as const)("%s", (_, key, apple, expected) => {
    expect(bindingFor(key, apple)).toBe(expected);
  });
});

describe("isApplePlatform", () => {
  test.each([
    ["userAgentData wins for Apple", { userAgentData: { platform: "macOS" }, platform: "Linux" }, true],
    ["userAgentData wins for Windows", { userAgentData: { platform: "Windows" }, platform: "MacIntel" }, false],
    ["legacy iPhone", { platform: "iPhone" }, true],
    ["legacy Windows", { platform: "Win32" }, false],
    ["missing platform", {}, false],
  ] as const)("%s", (_, source, expected) => {
    expect(isApplePlatform(source)).toBe(expected);
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

  test("a digit after the chord window is a bare digit", () => {
    const clock = fakeClock();
    const state = createKeyState(true, clock.timers);
    state.lookup(press("g"), null);
    clock.advance(1500);
    expect(state.lookup(press("1"), null)).toBe("1");
  });

  test("the scope chord is ignored while a field has focus", () => {
    const state = createKeyState(true, fakeClock().timers);
    const input = focused(["input"]);
    expect(state.lookup(press("g"), input)).toBeNull();
    expect(state.lookup(press("1"), input)).toBeNull();
    expect(state.lookup(press("1"), null)).toBe("1");
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
  test.each(["g", "t", "c"])("typing %s in a field reaches no binding", (key) => {
    const state = createKeyState(false, fakeClock().timers);
    expect(state.lookup(press(key), focused(["input"]))).toBeNull();
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

  test.each([
    ["link", focused(["a"]), null],
    ["button", focused(["button"]), null],
    ["summary", focused(["summary"]), null],
    ["div", focused(["div"]), "enter"],
    ["nothing", null, "enter"],
  ] as const)("Enter on %s", (_, target, expected) => {
    const state = createKeyState(false, fakeClock().timers);
    expect(state.lookup(press("Enter"), target)).toBe(expected);
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

  test("reaches a scope menu entry while its menu is closed, and opens the menu to click it", () => {
    const menu = { open: false };
    const entry = fake("g 1", { hidden: true, menu });
    expect(resolveTarget("g 1", [entry])).toBe(entry);
    activate(entry);
    expect(menu.open).toBe(true);
    expect(entry.calls).toEqual(["click"]);
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
  test.each([
    ["J from current", 1, 3, 1, [2]],
    ["K from current", 1, 3, -1, [0]],
    ["J without current", null, 3, 1, [0]],
    ["K without current", null, 3, -1, [2]],
    ["J past end", 2, 3, 1, []],
    ["K past start", 0, 3, -1, []],
    ["empty rail", null, 0, 1, []],
  ] as const)("%s", (_, current, count, step, clicks) => {
    const r = rail(current, count);
    expect(walkRail(r.links, step)).toBe(r.links[clicks[0] ?? -1] ?? null);
    expect(r.clicks).toEqual(clicks);
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
      events: new Array<string>(),
      blurs: 0,
      blur: () => void b.blurs++,
      dispatchEvent: (e: Event) => {
        b.events.push(e.type);
        return true;
      },
    };
    return b;
  }

  test.each([
    ["Enter with matches", { key: "Enter", query: "auth", matches: 2, handled: true, clicks: ["a"], value: "auth", events: [], blurs: 1 }],
    ["Enter without matches", { key: "Enter", query: "zzz", matches: 0, handled: true, clicks: [], value: "zzz", events: [], blurs: 0 }],
    ["Escape with query", { key: "Escape", query: "auth", matches: 0, handled: true, clicks: [], value: "", events: ["search"], blurs: 1 }],
    ["Escape without query", { key: "Escape", query: "", matches: 0, handled: true, clicks: [], value: "", events: [], blurs: 1 }],
    ["unhandled key", { key: "a", query: "x", matches: 0, handled: false, clicks: [], value: "x", events: [], blurs: 0 }],
  ] as const)("%s", (_, scenario) => {
    const b = box(scenario.query);
    const clicks: string[] = [];
    const links = Array.from({ length: scenario.matches }, (_, i) => ({ click: () => clicks.push(String.fromCharCode(97 + i)) }));
    expect(searchKey(scenario.key, b, links)).toBe(scenario.handled);
    expect(clicks).toEqual(scenario.clicks);
    expect(b.value).toBe(scenario.value);
    expect(b.events).toEqual(scenario.events);
    expect(b.blurs).toBe(scenario.blurs);
  });
});
