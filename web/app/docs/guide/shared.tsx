"use client";

// Building blocks shared by the written guide tabs. Each tab module is
// dynamically imported on first open, so anything here loads exactly once
// with whichever tab the reader opens first.

import { useState, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Copy, AlertTriangle, CheckCircle2, ChevronRight } from "lucide-react";

export type TFn = (key: string, params?: Record<string, string | number>) => string;
export type CopyFn = (text: string, label: string) => void;

export function CodeCard({ title, description, code, onCopy, t }: { title: string; description: string; code: string; onCopy: () => void; t: TFn }) {
  return (
    <Card className="overflow-hidden border-primary/10 bg-[#0b1020] text-slate-100 shadow-lg">
      <CardHeader className="border-b border-white/10">
        <CardTitle className="text-base text-white">{title}</CardTitle>
        <CardDescription className="text-slate-400">{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 p-4">
        <pre className="overflow-x-auto rounded-xl bg-black/30 p-4 text-xs leading-6 text-slate-200"><code>{code}</code></pre>
        <div className="flex justify-end"><Button variant="secondary" className="gap-2" onClick={onCopy}><Copy className="h-3.5 w-3.5" />{t("docs.copy")}</Button></div>
      </CardContent>
    </Card>
  );
}

export function SectionHeading({ icon, title, desc }: { icon: ReactNode; title: string; desc: string }) {
  return (
    <div className="mb-8">
      <div className="flex items-center gap-3 mb-2">{icon}<h2 className="text-2xl font-bold tracking-tight">{title}</h2></div>
      <p className="text-muted-foreground max-w-3xl">{desc}</p>
    </div>
  );
}

export function SubSection({ title, desc, children }: { title: string; desc?: string; children: ReactNode }) {
  return (
    <div className="mb-8">
      <h3 className="text-lg font-semibold mb-1">{title}</h3>
      {desc && <p className="text-sm text-muted-foreground mb-4">{desc}</p>}
      {children}
    </div>
  );
}

export function StepCard({ step, title, desc, children }: { step: number; title: string; desc: string; children?: ReactNode }) {
  return (
    <div className="rounded-xl border bg-background/90 p-5 shadow-sm">
      <div className="flex items-start gap-3">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground text-sm font-bold">{step}</div>
        <div className="flex-1 space-y-2">
          <h4 className="font-semibold">{title}</h4>
          <p className="text-sm text-muted-foreground">{desc}</p>
          {children}
        </div>
      </div>
    </div>
  );
}

export function EnvTable({ rows }: { rows: [string, string, string][] }) {
  return (
    <div className="overflow-x-auto rounded-xl border">
      <table className="w-full text-xs">
        <thead><tr className="border-b bg-muted/30"><th className="p-2 text-left font-semibold">Variable</th><th className="p-2 text-left font-semibold">Default</th><th className="p-2 text-left font-semibold">Description</th></tr></thead>
        <tbody>{rows.map(([v, d, n]) => <tr key={v} className="border-b last:border-0 hover:bg-muted/20"><td className="p-2 font-mono text-primary/90">{v}</td><td className="p-2 text-muted-foreground">{d}</td><td className="p-2">{n}</td></tr>)}</tbody>
      </table>
    </div>
  );
}

export function NoteBox({ children, variant = "info" }: { children: ReactNode; variant?: "info" | "warn" }) {
  const cls = variant === "warn" ? "border-amber-500/30 bg-amber-500/5" : "border-primary/20 bg-primary/5";
  const Icon = variant === "warn" ? AlertTriangle : CheckCircle2;
  const iconCls = variant === "warn" ? "text-amber-500" : "text-primary";
  return (
    <div className={`rounded-xl border p-4 ${cls}`}>
      <div className="flex gap-3"><Icon className={`h-5 w-5 shrink-0 mt-0.5 ${iconCls}`} /><div className="text-sm leading-relaxed">{children}</div></div>
    </div>
  );
}

export function Collapsible({ title, children, defaultOpen = false }: { title: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="rounded-xl border bg-background/90 overflow-hidden">
      <button onClick={() => setOpen(o => !o)} className="flex w-full items-center gap-3 p-4 text-left text-sm font-medium hover:bg-muted/30 transition-colors">
        <ChevronRight className={`h-4 w-4 text-primary shrink-0 transition-transform ${open ? "rotate-90" : ""}`} />
        {title}
      </button>
      {open && <div className="border-t px-4 pb-4 pt-3">{children}</div>}
    </div>
  );
}
