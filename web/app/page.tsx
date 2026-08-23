import type { Metadata } from "next";
import { HomeContent } from "./home-content";

export const metadata: Metadata = {
  title: "TabMail — Self-hosted temp mail",
  description:
    "Self-hosted disposable email with custom domains, multi-tenant isolation, and a clean REST API.",
};

// Server Component shell: static metadata lives here; the interactive,
// locale-aware body renders on the client.
export default function HomePage() {
  return <HomeContent />;
}
