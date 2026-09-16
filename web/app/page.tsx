import type { Metadata } from "next";
import { HomeContent } from "./home-content";

export const metadata: Metadata = {
  title: "TabMail — Company Mail",
  description:
    "Private company email with employee and shared mailboxes, explicit permissions, administrator templates and traceable delivery.",
};

// Server Component shell: static metadata lives here; the interactive,
// locale-aware body renders on the client.
export default function HomePage() {
  return <HomeContent />;
}
