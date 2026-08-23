import type { Metadata } from "next";
import { DocsContent } from "./docs-content";

export const metadata: Metadata = {
  title: "API Docs — TabMail",
  description:
    "TabMail REST API reference: Swagger UI, ReDoc, quickstart, deployment, domain routing, and operations guides.",
};

// Server Component shell: static metadata lives here; the interactive,
// locale-aware body renders on the client.
export default function DocsPage() {
  return <DocsContent />;
}
