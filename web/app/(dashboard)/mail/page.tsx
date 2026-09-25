import { Suspense } from "react";
import { MailWorkspace } from "@/features/mail/workspace";
export default function MailPage() {
 return <Suspense fallback={<main className="p-7">Loading…</main>}><MailWorkspace/></Suspense>;
}
