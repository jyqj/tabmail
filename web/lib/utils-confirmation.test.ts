import { afterEach, describe, expect, it, vi } from "vitest";
import { safeConfirm } from "./utils";

afterEach(() => vi.unstubAllGlobals());

describe("destructive action confirmation", () => {
  it("requires a browser window", () => {
    vi.stubGlobal("window", undefined);
    expect(safeConfirm("Delete this resource?")).toBe(false);
  });

  it.each([undefined, null, false, "confirm"])(
    "refuses unavailable or non-callable confirmation: %s",
    (confirm) => {
      vi.stubGlobal("window", { confirm });
      expect(safeConfirm("Delete this resource?")).toBe(false);
    },
  );

  it("refuses a confirmation API that throws", () => {
    vi.stubGlobal("window", { confirm: () => { throw new Error("dialog blocked"); } });
    expect(safeConfirm("Delete this resource?")).toBe(false);
  });

  it("refuses a confirmation property that cannot be read", () => {
    vi.stubGlobal("window", Object.defineProperty({}, "confirm", {
      get: () => { throw new Error("dialog unavailable"); },
    }));
    expect(safeConfirm("Delete this resource?")).toBe(false);
  });

  it.each([false, undefined, null, 0, 1, "true", {}, Promise.resolve(true)])(
    "refuses every result except the boolean true: %s",
    (result) => {
      vi.stubGlobal("window", { confirm: vi.fn(() => result) });
      expect(safeConfirm("Delete this resource?")).toBe(false);
    },
  );

  it("preserves the message and receiver when the user explicitly confirms", () => {
    const message = "This change is irreversible. Continue?";
    const confirm = vi.fn(function (this: unknown, received: string) {
      expect(this).toBe(browser);
      expect(received).toBe(message);
      return true;
    });
    const browser = { confirm };
    vi.stubGlobal("window", browser);
    expect(safeConfirm(message)).toBe(true);
    expect(confirm).toHaveBeenCalledExactlyOnceWith(message);
  });
});
