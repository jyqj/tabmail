import "@testing-library/jest-dom/vitest";
import React from "react";
import { beforeEach, vi } from "vitest";
import { SWRConfig } from "swr";

// Non-default catalogs are code split, so assertions on English copy would race
// the dynamic import unless the catalog is resolved before the first render.
// The locale has to be stored before the module loads, because that is when the
// i18n store adopts it.
window.localStorage.setItem("tabmail-locale", "en");
const { I18nProvider, preloadLocale } = await import("@/lib/i18n");
await preloadLocale("en");

vi.mock("@testing-library/react", async () => {
  const actual = await vi.importActual<typeof import("@testing-library/react")>("@testing-library/react");

  return {
    ...actual,
    render: (ui: React.ReactElement, options?: Parameters<typeof actual.render>[1]) =>
      actual.render(
        React.createElement(
          SWRConfig,
          { value: { provider: () => new Map() } },
          React.createElement(I18nProvider, null, ui)
        ),
        options
      ),
  };
});

beforeEach(() => {
  window.localStorage.clear();
  window.localStorage.setItem("tabmail-locale", "en");
});

