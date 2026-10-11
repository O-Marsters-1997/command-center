import { describe, expect, test } from "bun:test";
import {
  activate,
  bindingFor,
  createKeyState,
  type Focused,
  isApplePlatform,
  type KeyInfo,
  type KeyTarget,
  resolveTarget,
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

describe("activate", () => {
  test("focuses a field and clicks anything else", () => {
    const field = fake("mod+k", { field: true });
    const link = fake("g h");
    activate(field);
    activate(link);
    expect(field.calls).toEqual(["focus"]);
    expect(link.calls).toEqual(["click"]);
  });
});
