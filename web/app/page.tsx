"use client";

import { useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { SiteHeader } from "@/components/site-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import {
  Mail,
  ArrowRight,
  Globe,
  Zap,
  ShieldCheck,
  Clock,
  Layers,
  Code2,
  Shuffle,
} from "lucide-react";

const FEATURES = [
  {
    icon: Globe,
    title: "Custom Domains",
    desc: "Bind any domain. Configure wildcard and sequence routes for batch subdomain emails.",
  },
  {
    icon: ShieldCheck,
    title: "Flexible Access",
    desc: "Public, token, or API-key access per mailbox. Fine-grained control for every use case.",
  },
  {
    icon: Clock,
    title: "Auto Cleanup",
    desc: "Configurable retention per mailbox, route, tenant, or plan. 4-level priority cascade.",
  },
  {
    icon: Layers,
    title: "Multi-Tenancy",
    desc: "Full tenant isolation with plans, overrides, API keys, and scoped permissions.",
  },
  {
    icon: Zap,
    title: "High Performance",
    desc: "Go backend with goroutine-per-connection SMTP. Redis rate limiting. Built for scale.",
  },
  {
    icon: Code2,
    title: "API First",
    desc: "OpenAPI 3.1 spec as single source of truth. Swagger UI, ReDoc, and typed SDKs.",
  },
];

function randomAddress(): string {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  let s = "";
  for (let i = 0; i < 8; i++) s += chars[Math.floor(Math.random() * chars.length)];
  return `${s}@tabmail.dev`;
}

export default function HomePage() {
  const router = useRouter();
  const [address, setAddress] = useState("");

  const go = useCallback(() => {
    const target = address.trim();
    if (!target) return;
    router.push(`/inbox/${encodeURIComponent(target)}`);
  }, [address, router]);

  const handleRandom = () => {
    const addr = randomAddress();
    setAddress(addr);
    router.push(`/inbox/${encodeURIComponent(addr)}`);
  };

  return (
    <div className="flex min-h-screen flex-col">
      <SiteHeader />

      <main className="flex-1">
        {/* Hero */}
        <section className="relative overflow-hidden">
          <div className="absolute inset-0 -z-10 bg-[radial-gradient(ellipse_60%_50%_at_50%_-20%,var(--color-primary)/8%,transparent)]" />
          <div className="container mx-auto max-w-6xl px-4 pt-24 pb-16 text-center">
            <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl bg-primary/10 mb-6">
              <Mail className="h-8 w-8 text-primary" />
            </div>
            <h1 className="text-4xl font-bold tracking-tight sm:text-5xl lg:text-6xl">
              Disposable email,
              <br />
              <span className="text-muted-foreground">your rules.</span>
            </h1>
            <p className="mx-auto mt-4 max-w-xl text-lg text-muted-foreground">
              Self-hosted temporary email with custom domains, multi-tenancy,
              and a clean API. No sign-up required.
            </p>

            <div className="mx-auto mt-10 flex max-w-lg flex-col gap-3 sm:flex-row">
              <div className="relative flex-1">
                <Mail className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  className="h-12 pl-10 text-base"
                  placeholder="anything@yourdomain.com"
                  value={address}
                  onChange={(e) => setAddress(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && go()}
                />
              </div>
              <Button className="h-12 px-6 gap-2" onClick={go} disabled={!address.trim()}>
                Open Inbox
                <ArrowRight className="h-4 w-4" />
              </Button>
            </div>

            <button
              onClick={handleRandom}
              className="mt-4 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            >
              <Shuffle className="h-3.5 w-3.5" />
              Generate random address
            </button>
          </div>
        </section>

        {/* Features */}
        <section className="border-t bg-muted/30">
          <div className="container mx-auto max-w-6xl px-4 py-20">
            <h2 className="text-center text-2xl font-semibold tracking-tight sm:text-3xl">
              Built for developers
            </h2>
            <p className="mx-auto mt-2 max-w-lg text-center text-muted-foreground">
              Everything you need for a production-grade temporary email system.
            </p>
            <div className="mt-12 grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
              {FEATURES.map((f) => (
                <Card
                  key={f.title}
                  className="group border-transparent bg-background shadow-sm hover:shadow-md transition-shadow"
                >
                  <CardContent className="pt-6">
                    <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary mb-4 group-hover:bg-primary/15 transition-colors">
                      <f.icon className="h-5 w-5" />
                    </div>
                    <h3 className="font-semibold">{f.title}</h3>
                    <p className="mt-1.5 text-sm text-muted-foreground leading-relaxed">
                      {f.desc}
                    </p>
                  </CardContent>
                </Card>
              ))}
            </div>
          </div>
        </section>
      </main>

      <footer className="border-t py-6 text-center text-sm text-muted-foreground">
        <div className="container mx-auto max-w-6xl px-4">
          TabMail &mdash; Self-hosted temporary email system
        </div>
      </footer>
    </div>
  );
}
