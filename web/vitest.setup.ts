import "@testing-library/jest-dom/vitest";
import React from "react";
import { beforeEach, vi } from "vitest";

import { I18nProvider } from "@/lib/i18n";

vi.mock("@testing-library/react", async () => {
  const actual = await vi.importActual<typeof import("@testing-library/react")>("@testing-library/react");

  return {
    ...actual,
    render: (ui: React.ReactElement, options?: Parameters<typeof actual.render>[1]) =>
      actual.render(React.createElement(I18nProvider, null, ui), options),
  };
});

beforeEach(() => {
  window.localStorage.clear();
  window.localStorage.setItem("tabmail-locale", "en");
});
