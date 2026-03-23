"use client";

import { useState, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { SiteHeader } from "@/components/site-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { useI18n } from "@/lib/i18n";
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
  Server,
  Inbox,
  Send,
  BookOpen,
  ChevronDown,
} from "lucide-react";
import { cn } from "@/lib/utils";

function randomAddress(): string {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  let s = "";
  for (let i = 0; i < 8; i++) s += chars[Math.floor(Math.random() * chars.length)];
  return `${s}@tabmail.dev`;
}

function InboxMockup() {
  const { t } = useI18n();

  const emails = [
    {
      initials: "GH",
      color: "bg-violet-500/15 text-violet-600 dark:text-violet-400",
      sender: t("mock.sender1"),
      subject: t("mock.subject1"),
      time: t("mock.time1"),
      unread: true,
    },
    {
      initials: "ST",
      color: "bg-blue-500/15 text-blue-600 dark:text-blue-400",
      sender: t("mock.sender2"),
      subject: t("mock.subject2"),
      time: t("mock.time2"),
      unread: true,
    },
    {
      initials: "TM",
      color: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
      sender: t("mock.sender3"),
      subject: t("mock.subject3"),
      time: t("mock.time3"),
      unread: false,
    },
  ];

  return (
    <div className="relative mt-16 mx-auto max-w-2xl">
      <div className="absolute -inset-6 bg-gradient-to-b from-primary/[0.07] to-transparent rounded-3xl blur-2xl" />
      <div className="relative rounded-xl border bg-background shadow-2xl overflow-hidden">
        {/* Title bar */}
        <div className="flex items-center gap-2.5 border-b px-4 py-2.5 bg-muted/40">
          <div className="flex gap-1.5">
            <span className="h-3 w-3 rounded-full bg-rose-500/60" />
            <span className="h-3 w-3 rounded-full bg-amber-500/60" />
            <span className="h-3 w-3 rounded-full bg-emerald-500/60" />
          </div>
          <div className="flex items-center gap-1.5 ml-1">
            <Inbox className="h-3 w-3 text-muted-foreground" />
            <code className="text-xs text-muted-foreground">
              {t("mock.inbox")} &mdash; test@yourdomain.com
            </code>
          </div>
        </div>
        {/* Fake messages */}
        <div className="divide-y">
          {emails.map((email) => (
            <div
              key={email.initials}
              className="flex items-center gap-3 px-4 py-3 hover:bg-muted/30 transition-colors"
            >
              <div
                className={`h-8 w-8 rounded-full flex items-center justify-center text-[11px] font-semibold shrink-0 ${email.color}`}
              >
                {email.initials}
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center justify-between gap-2">
                  <span className={`text-sm ${email.unread ? "font-semibold" : "text-muted-foreground"}`}>
                    {email.sender}
                  </span>
                  <span className="text-[11px] text-muted-foreground shrink-0">
                    {email.time}
                  </span>
                </div>
                <p className={`text-xs truncate mt-0.5 ${email.unread ? "text-foreground" : "text-muted-foreground"}`}>
                  {email.subject}
                </p>
              </div>
              {email.unread && (
                <span className="h-2 w-2 rounded-full bg-primary shrink-0" />
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function FAQItem({ question, answer }: { question: string; answer: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between gap-4 px-5 py-4 text-left hover:bg-muted/30 transition-colors cursor-pointer"
      >
        <span className="text-sm font-medium">{question}</span>
        <ChevronDown className={cn("h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200", open && "rotate-180")} />
      </button>
      {open && (
        <div className="px-5 pb-4 text-sm text-muted-foreground leading-relaxed">
          {answer}
        </div>
      )}
    </div>
  );
}

export default function HomePage() {
  const router = useRouter();
  const { t } = useI18n();
  const [address, setAddress] = useState("");
  const heroInputRef = useRef<HTMLInputElement>(null);

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

  const FEATURES = [
    { icon: Globe, titleKey: "home.feat.domains", descKey: "home.feat.domainsDesc", accent: "from-blue-500/10 to-transparent" },
    { icon: ShieldCheck, titleKey: "home.feat.access", descKey: "home.feat.accessDesc", accent: "from-emerald-500/10 to-transparent" },
    { icon: Clock, titleKey: "home.feat.cleanup", descKey: "home.feat.cleanupDesc", accent: "from-amber-500/10 to-transparent" },
    { icon: Layers, titleKey: "home.feat.tenancy", descKey: "home.feat.tenancyDesc", accent: "from-violet-500/10 to-transparent" },
    { icon: Zap, titleKey: "home.feat.perf", descKey: "home.feat.perfDesc", accent: "from-rose-500/10 to-transparent" },
    { icon: Code2, titleKey: "home.feat.api", descKey: "home.feat.apiDesc", accent: "from-cyan-500/10 to-transparent" },
  ];

  const STEPS = [
    { step: "01", icon: Server, titleKey: "home.step01", descKey: "home.step01Desc" },
    { step: "02", icon: Send, titleKey: "home.step02", descKey: "home.step02Desc" },
    { step: "03", icon: Inbox, titleKey: "home.step03", descKey: "home.step03Desc" },
  ];

  return (
    <div className="flex min-h-screen flex-col">
      <SiteHeader />

      <main className="flex-1">
        {/* Hero */}
        <section className="relative overflow-hidden">
          <div className="absolute inset-0 -z-10 bg-[radial-gradient(ellipse_80%_50%_at_50%_-20%,var(--color-primary)/10%,transparent)]" />
          <div className="absolute inset-0 -z-10 bg-[linear-gradient(to_right,var(--color-border)/30%_1px,transparent_1px),linear-gradient(to_bottom,var(--color-border)/30%_1px,transparent_1px)] bg-[size:4rem_4rem] [mask-image:radial-gradient(ellipse_60%_60%_at_50%_30%,black_20%,transparent_100%)]" />

          <div className="container mx-auto max-w-6xl px-4 pt-28 pb-8 text-center">
            <div className="inline-flex items-center gap-2 rounded-full border bg-muted/50 px-4 py-1.5 text-xs text-muted-foreground mb-8">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse" />
              {t("home.badge")}
            </div>

            <h1 className="text-4xl font-bold tracking-tight sm:text-5xl lg:text-6xl leading-[1.1]">
              {t("home.title1")}
              <br />
              <span className="bg-gradient-to-r from-foreground/80 to-foreground/40 bg-clip-text text-transparent">
                {t("home.title2")}
              </span>
            </h1>
            <p className="mx-auto mt-5 max-w-xl text-lg text-muted-foreground leading-relaxed">
              {t("home.desc")}
            </p>

            <div className="mx-auto mt-10 flex max-w-lg flex-col gap-3 sm:flex-row">
              <div className="relative flex-1">
                <Mail className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  ref={heroInputRef}
                  className="h-12 pl-10 text-base"
                  placeholder={t("home.placeholder")}
                  value={address}
                  onChange={(e) => setAddress(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && go()}
                />
              </div>
              <Button className="h-12 px-6 gap-2" onClick={go} disabled={!address.trim()}>
                {t("home.openInbox")}
                <ArrowRight className="h-4 w-4" />
              </Button>
            </div>

            <button
              onClick={handleRandom}
              className="mt-4 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            >
              <Shuffle className="h-3.5 w-3.5" />
              {t("home.random")}
            </button>

            <InboxMockup />
          </div>
        </section>

        {/* How it works */}
        <section className="border-t">
          <div className="container mx-auto max-w-6xl px-4 py-20">
            <div className="text-center mb-14">
              <p className="text-xs font-medium uppercase tracking-widest text-muted-foreground mb-2">
                {t("home.howItWorks")}
              </p>
              <h2 className="text-2xl font-semibold tracking-tight sm:text-3xl">
                {t("home.threeSteps")}
              </h2>
            </div>

            <div className="grid gap-8 md:grid-cols-3">
              {STEPS.map((s, i) => (
                <div key={s.step} className="relative flex flex-col items-center text-center">
                  {i < STEPS.length - 1 && (
                    <div className="absolute top-8 left-[calc(50%+2.5rem)] hidden w-[calc(100%-5rem)] border-t border-dashed border-border md:block" />
                  )}
                  <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-muted mb-5">
                    <s.icon className="h-7 w-7 text-foreground/70" />
                  </div>
                  <span className="text-[11px] font-semibold uppercase tracking-widest text-muted-foreground mb-2">
                    Step {s.step}
                  </span>
                  <h3 className="font-semibold text-[15px] mb-1.5">{t(s.titleKey)}</h3>
                  <p className="text-sm text-muted-foreground leading-relaxed max-w-xs">
                    {t(s.descKey)}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* Features */}
        <section className="border-t bg-muted/30">
          <div className="container mx-auto max-w-6xl px-4 py-20">
            <div className="text-center mb-14">
              <p className="text-xs font-medium uppercase tracking-widest text-muted-foreground mb-2">
                {t("home.features")}
              </p>
              <h2 className="text-2xl font-semibold tracking-tight sm:text-3xl">
                {t("home.featuresTitle")}
              </h2>
              <p className="mx-auto mt-2 max-w-lg text-muted-foreground">
                {t("home.featuresDesc")}
              </p>
            </div>

            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {FEATURES.map((f) => (
                <Card
                  key={f.titleKey}
                  className="group relative overflow-hidden border-transparent bg-background shadow-sm hover:shadow-md transition-all hover:-translate-y-0.5"
                >
                  <div className={`absolute inset-0 bg-gradient-to-br ${f.accent} opacity-0 group-hover:opacity-100 transition-opacity`} />
                  <CardContent className="relative pt-6">
                    <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary mb-4 group-hover:bg-primary/15 transition-colors">
                      <f.icon className="h-5 w-5" />
                    </div>
                    <h3 className="font-semibold">{t(f.titleKey)}</h3>
                    <p className="mt-1.5 text-sm text-muted-foreground leading-relaxed">
                      {t(f.descKey)}
                    </p>
                  </CardContent>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* CTA */}
        <section className="border-t">
          <div className="container mx-auto max-w-6xl px-4 py-16">
            <div className="relative overflow-hidden rounded-2xl bg-primary/[0.04] border border-primary/10 px-8 py-12 text-center">
              <div className="absolute inset-0 bg-[radial-gradient(circle_at_50%_120%,var(--color-primary)/8%,transparent_70%)]" />
              <div className="relative">
                <h2 className="text-xl font-semibold sm:text-2xl">{t("home.ctaTitle")}</h2>
                <p className="mt-2 text-muted-foreground max-w-md mx-auto">
                  {t("home.ctaDesc")}
                </p>
                <div className="mt-6 flex flex-col sm:flex-row items-center justify-center gap-3">
                  <Button className="gap-2" onClick={() => heroInputRef.current?.focus()}>
                    <Mail className="h-4 w-4" />
                    {t("home.ctaTry")}
                  </Button>
                  <Button variant="outline" className="gap-2" render={<Link href="/docs" />}>
                    <BookOpen className="h-4 w-4" />
                    {t("home.ctaDocs")}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </section>
        {/* FAQ */}
        <section className="border-t">
          <div className="container mx-auto max-w-3xl px-4 py-20">
            <div className="text-center mb-12">
              <p className="text-xs font-medium uppercase tracking-widest text-muted-foreground mb-2">
                {t("faq.title")}
              </p>
              <h2 className="text-2xl font-semibold tracking-tight sm:text-3xl">
                {t("faq.title")}
              </h2>
            </div>
            <div className="divide-y rounded-xl border">
              {(["1", "2", "3", "4", "5"] as const).map((n) => (
                <FAQItem key={n} question={t(`faq.q${n}`)} answer={t(`faq.a${n}`)} />
              ))}
            </div>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="border-t">
        <div className="container mx-auto max-w-6xl px-4 py-8">
          <div className="flex flex-col md:flex-row items-center justify-between gap-4">
            <div className="flex items-center gap-2.5">
              <div className="flex h-6 w-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
                <Mail className="h-3 w-3" />
              </div>
              <span className="text-sm font-medium">TabMail</span>
              <span className="text-xs text-muted-foreground">
                &mdash; {t("home.tagline")}
              </span>
            </div>
            <nav className="flex items-center gap-4 text-xs text-muted-foreground">
              <Link href="/docs" className="hover:text-foreground transition-colors">
                {t("header.docs")}
              </Link>
              <span className="text-border">|</span>
              <span>&copy; {new Date().getFullYear()} TabMail</span>
            </nav>
          </div>
        </div>
      </footer>
    </div>
  );
}
