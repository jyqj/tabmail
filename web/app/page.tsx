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
      color: "bg-violet-500/15 text-violet-600 dark:text-violet-400 border border-violet-500/20",
      sender: t("mock.sender1"),
      subject: t("mock.subject1"),
      time: t("mock.time1"),
      unread: true,
    },
    {
      initials: "ST",
      color: "bg-cyan-500/15 text-cyan-600 dark:text-cyan-400 border border-cyan-500/20",
      sender: t("mock.sender2"),
      subject: t("mock.subject2"),
      time: t("mock.time2"),
      unread: true,
    },
    {
      initials: "TM",
      color: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20",
      sender: t("mock.sender3"),
      subject: t("mock.subject3"),
      time: t("mock.time3"),
      unread: false,
    },
  ];

  return (
    <div className="relative mt-16 mx-auto max-w-2xl transform hover:scale-[1.01] transition-transform duration-500 ease-out">
      {/* Neon Glow */}
      <div className="absolute -inset-1 bg-gradient-to-br from-primary/30 via-transparent to-secondary/30 rounded-3xl blur-2xl opacity-70" />
      <div className="relative rounded-2xl border border-border/50 bg-background/80 backdrop-blur-3xl shadow-[0_8px_32px_rgba(0,0,0,0.1)] overflow-hidden">
        {/* Title bar */}
        <div className="flex items-center gap-2.5 border-b border-border/40 px-5 py-3.5 bg-muted/20">
          <div className="flex gap-2">
            <span className="h-3 w-3 rounded-full bg-destructive/80 shadow-[0_0_8px_rgba(var(--color-destructive),0.5)]" />
            <span className="h-3 w-3 rounded-full bg-amber-500/80 shadow-[0_0_8px_rgba(245,158,11,0.5)]" />
            <span className="h-3 w-3 rounded-full bg-emerald-500/80 shadow-[0_0_8px_rgba(16,185,129,0.5)]" />
          </div>
          <div className="flex items-center gap-2 ml-2">
            <Inbox className="h-3 w-3 text-muted-foreground" />
            <code className="text-[11px] uppercase tracking-wider font-mono text-muted-foreground">
              {t("mock.inbox")} &mdash; test@yourdomain.com
            </code>
          </div>
        </div>
        {/* Fake messages */}
        <div className="divide-y divide-border/30">
          {emails.map((email) => (
            <div
              key={email.initials}
              className="flex items-center gap-4 px-5 py-4 hover:bg-muted/40 transition-colors cursor-default"
            >
              <div
                className={`h-10 w-10 rounded-xl flex items-center justify-center text-xs font-bold shrink-0 ${email.color}`}
              >
                {email.initials}
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center justify-between gap-2">
                  <span className={`text-sm ${email.unread ? "font-bold text-foreground" : "font-medium text-muted-foreground"}`}>
                    {email.sender}
                  </span>
                  <span className="text-[10px] uppercase tracking-wider font-mono text-muted-foreground shrink-0">
                    {email.time}
                  </span>
                </div>
                <p className={`text-sm truncate mt-1 ${email.unread ? "text-foreground/90 font-medium" : "text-muted-foreground"}`}>
                  {email.subject}
                </p>
              </div>
              {email.unread && (
                <span className="h-2 w-2 rounded-full bg-primary shadow-[0_0_8px_rgba(var(--color-primary),0.8)] shrink-0" />
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
    <div className="group border border-transparent hover:border-border/50 rounded-lg transition-colors">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between gap-4 px-5 py-5 text-left cursor-pointer"
      >
        <span className="text-base font-semibold group-hover:text-primary transition-colors">{question}</span>
        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted/50 group-hover:bg-primary/10 transition-colors shrink-0">
          <ChevronDown className={cn("h-4 w-4 text-muted-foreground group-hover:text-primary transition-transform duration-300", open && "rotate-180")} />
        </div>
      </button>
      {open && (
        <div className="px-5 pb-5 text-sm text-muted-foreground leading-relaxed animate-in slide-in-from-top-2 fade-in duration-200">
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
    { icon: Globe, titleKey: "home.feat.domains", descKey: "home.feat.domainsDesc", accent: "from-blue-500/20 to-transparent" },
    { icon: ShieldCheck, titleKey: "home.feat.access", descKey: "home.feat.accessDesc", accent: "from-emerald-500/20 to-transparent" },
    { icon: Clock, titleKey: "home.feat.cleanup", descKey: "home.feat.cleanupDesc", accent: "from-amber-500/20 to-transparent" },
    { icon: Layers, titleKey: "home.feat.tenancy", descKey: "home.feat.tenancyDesc", accent: "from-violet-500/20 to-transparent" },
    { icon: Zap, titleKey: "home.feat.perf", descKey: "home.feat.perfDesc", accent: "from-rose-500/20 to-transparent" },
    { icon: Code2, titleKey: "home.feat.api", descKey: "home.feat.apiDesc", accent: "from-cyan-500/20 to-transparent" },
  ];

  const STEPS = [
    { step: "01", icon: Server, titleKey: "home.step01", descKey: "home.step01Desc" },
    { step: "02", icon: Send, titleKey: "home.step02", descKey: "home.step02Desc" },
    { step: "03", icon: Inbox, titleKey: "home.step03", descKey: "home.step03Desc" },
  ];

  return (
    <div className="flex min-h-screen flex-col selection:bg-primary/30 selection:text-foreground">
      <SiteHeader />

      <main className="flex-1">
        {/* Hero */}
        <section className="relative overflow-hidden pt-36 pb-16 md:pt-48 md:pb-24">
          <div className="absolute inset-0 -z-10 bg-[radial-gradient(ellipse_100%_100%_at_50%_-20%,rgba(var(--color-primary),0.15),transparent)]" />
          <div className="absolute inset-0 -z-10 bg-[#000000] dark:bg-transparent bg-[linear-gradient(to_right,rgba(255,255,255,0.05)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,0.05)_1px,transparent_1px)] bg-[size:3rem_3rem] [mask-image:radial-gradient(ellipse_60%_60%_at_50%_0%,black_30%,transparent_100%)] dark:bg-[linear-gradient(to_right,rgba(255,255,255,0.02)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,0.02)_1px,transparent_1px)]" />
          <div className="container mx-auto max-w-6xl px-4 text-center">
            
            <div className="animate-in fade-in slide-in-from-bottom-4 duration-700 inline-flex items-center gap-3 rounded-full border border-border/50 bg-background/50 backdrop-blur-md px-5 py-2 text-xs font-mono font-medium text-foreground mb-10 shadow-sm">
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
              </span>
              {t("home.badge")}
            </div>

            <h1 className="animate-in fade-in slide-in-from-bottom-6 duration-700 delay-150 text-5xl font-heading font-black tracking-tighter sm:text-6xl lg:text-8xl leading-[1.05]">
              {t("home.title1")}
              <br />
              <span className="bg-gradient-to-br from-primary via-primary/80 to-secondary bg-clip-text text-transparent filter drop-shadow-sm">
                {t("home.title2")}
              </span>
            </h1>
            
            <p className="animate-in fade-in slide-in-from-bottom-8 duration-700 delay-300 mx-auto mt-8 max-w-2xl text-lg md:text-xl text-muted-foreground leading-relaxed font-light">
              {t("home.desc")}
            </p>

            <div className="animate-in fade-in slide-in-from-bottom-8 duration-700 delay-500 mx-auto mt-12 flex max-w-xl flex-col gap-4 sm:flex-row shadow-2xl shadow-primary/5 rounded-2xl p-2 bg-background/40 backdrop-blur-xl border border-border/50">
              <div className="relative flex-1">
                <Mail className="absolute left-4 top-1/2 h-5 w-5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  ref={heroInputRef}
                  className="h-14 pl-12 text-lg border-transparent bg-muted/30 focus-visible:ring-1 focus-visible:ring-primary shadow-inner rounded-xl"
                  placeholder={t("home.placeholder")}
                  value={address}
                  onChange={(e) => setAddress(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && go()}
                />
              </div>
              <Button className="h-14 px-8 gap-2 text-base font-semibold rounded-xl" onClick={go} disabled={!address.trim()}>
                {t("home.openInbox")}
                <ArrowRight className="h-4 w-4" />
              </Button>
            </div>

            <button
              onClick={handleRandom}
              className="animate-in fade-in duration-1000 delay-700 mt-6 md:mt-8 inline-flex items-center gap-2 text-sm font-medium text-muted-foreground hover:text-primary transition-colors cursor-pointer"
            >
              <Shuffle className="h-4 w-4" />
              {t("home.random")}
            </button>

            <div className="animate-in fade-in slide-in-from-bottom-12 duration-1000 delay-700">
              <InboxMockup />
            </div>
          </div>
        </section>

        {/* How it works */}
        <section className="relative py-24 md:py-32 overflow-hidden border-t border-border/40">
          <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-primary/20 to-transparent" />
          <div className="container mx-auto max-w-6xl px-4">
            <div className="text-center mb-20">
              <h2 className="text-sm font-mono font-bold uppercase tracking-[0.2em] text-primary mb-4">
                {t("home.howItWorks")}
              </h2>
              <p className="text-3xl font-heading font-bold tracking-tight sm:text-4xl md:text-5xl">
                {t("home.threeSteps")}
              </p>
            </div>

            <div className="grid gap-12 md:gap-8 md:grid-cols-3">
              {STEPS.map((s, i) => (
                <div key={s.step} className="relative flex flex-col items-center text-center group">
                  {i < STEPS.length - 1 && (
                    <div className="absolute top-12 left-[calc(50%+3rem)] hidden w-[calc(100%-6rem)] border-t-2 border-dashed border-border/60 md:block group-hover:border-primary/50 transition-colors" />
                  )}
                  <div className="relative flex h-24 w-24 items-center justify-center rounded-3xl bg-background border border-border/50 shadow-sm mb-6 group-hover:shadow-[0_0_30px_rgba(var(--color-primary),0.15)] group-hover:border-primary/30 transition-all duration-500">
                    <div className="absolute inset-0 rounded-3xl bg-gradient-to-br from-primary/5 to-transparent opacity-0 group-hover:opacity-100" />
                    <s.icon className="h-10 w-10 text-muted-foreground group-hover:text-primary transition-colors duration-500" />
                  </div>
                  <span className="text-xs font-mono font-bold uppercase tracking-widest text-muted-foreground mb-3">
                    Step {s.step}
                  </span>
                  <h3 className="font-heading font-bold text-xl mb-3">{t(s.titleKey)}</h3>
                  <p className="text-base text-muted-foreground leading-relaxed max-w-xs">
                    {t(s.descKey)}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* Features */}
        <section className="relative py-24 md:py-32 bg-muted/10 border-t border-border/40">
          <div className="container mx-auto max-w-6xl px-4">
            <div className="text-center mb-20">
              <h2 className="text-sm font-mono font-bold uppercase tracking-[0.2em] text-primary mb-4">
                {t("home.features")}
              </h2>
              <p className="text-3xl font-heading font-bold tracking-tight sm:text-4xl md:text-5xl mb-6">
                {t("home.featuresTitle")}
              </p>
              <p className="mx-auto max-w-2xl text-lg text-muted-foreground">
                {t("home.featuresDesc")}
              </p>
            </div>

            <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
              {FEATURES.map((f) => (
                <Card
                  key={f.titleKey}
                  className="group relative overflow-hidden bg-background border border-border/40 shadow-sm hover:shadow-xl hover:shadow-primary/5 hover:border-primary/20 transition-all duration-500"
                >
                  <div className={`absolute inset-0 bg-gradient-to-br ${f.accent} opacity-0 group-hover:opacity-100 transition-opacity duration-500`} />
                  <CardContent className="relative p-8">
                    <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-muted/50 text-foreground mb-6 group-hover:bg-primary group-hover:text-primary-foreground transition-colors duration-500 shadow-sm">
                      <f.icon className="h-6 w-6" />
                    </div>
                    <h3 className="font-heading font-bold text-lg mb-2">{t(f.titleKey)}</h3>
                    <p className="text-sm text-muted-foreground leading-relaxed">
                      {t(f.descKey)}
                    </p>
                  </CardContent>
                </Card>
              ))}
            </div>
          </div>
        </section>

        {/* CTA */}
        <section className="relative py-24 md:py-32 border-t border-border/40 overflow-hidden">
          <div className="absolute inset-0 bg-primary/5" />
          <div className="absolute top-0 right-[-10%] w-[50%] h-[50%] bg-secondary/10 blur-[100px] rounded-full" />
          <div className="absolute bottom-[-10%] left-[-10%] w-[50%] h-[50%] bg-primary/10 blur-[100px] rounded-full" />
          
          <div className="container mx-auto max-w-4xl px-4 text-center relative z-10">
            <h2 className="text-3xl font-heading font-black tracking-tighter md:text-5xl lg:text-6xl mb-6 drop-shadow-sm">
              {t("home.ctaTitle")}
            </h2>
            <p className="text-lg md:text-xl text-muted-foreground max-w-2xl mx-auto mb-10">
              {t("home.ctaDesc")}
            </p>
            <div className="flex flex-col sm:flex-row items-center justify-center gap-4">
              <Button size="lg" className="h-14 px-8 gap-2 text-base font-semibold w-full sm:w-auto shadow-[0_0_20px_rgba(var(--color-primary),0.3)] hover:shadow-[0_0_30px_rgba(var(--color-primary),0.5)] transition-shadow" onClick={() => heroInputRef.current?.focus()}>
                <Mail className="h-5 w-5" />
                {t("home.ctaTry")}
              </Button>
              <Button variant="outline" size="lg" className="h-14 px-8 gap-2 text-base font-semibold w-full sm:w-auto bg-background/50 backdrop-blur-sm" render={<Link href="/docs" />}>
                <BookOpen className="h-5 w-5" />
                {t("home.ctaDocs")}
              </Button>
            </div>
          </div>
        </section>
        
        {/* FAQ */}
        <section className="py-24 md:py-32 border-t border-border/40">
          <div className="container mx-auto max-w-3xl px-4">
            <div className="text-center mb-16">
              <h2 className="text-sm font-mono font-bold uppercase tracking-[0.2em] text-primary mb-4">
                FAQ
              </h2>
              <p className="text-3xl font-heading font-bold tracking-tight sm:text-4xl">
                {t("faq.title")}
              </p>
            </div>
            <div className="flex flex-col gap-3">
              {(["1", "2", "3", "4", "5"] as const).map((n) => (
                <FAQItem key={n} question={t(`faq.q${n}`)} answer={t(`faq.a${n}`)} />
              ))}
            </div>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="border-t border-border/40 bg-background/80 backdrop-blur-lg">
        <div className="container mx-auto max-w-6xl px-4 py-10">
          <div className="flex flex-col md:flex-row items-center justify-between gap-6">
            <div className="flex items-center gap-3">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-[0_0_10px_rgba(var(--color-primary),0.3)]">
                <Mail className="h-4 w-4" />
              </div>
              <span className="font-heading font-bold text-lg">TabMail</span>
            </div>
            <nav className="flex items-center gap-6 text-sm font-medium text-muted-foreground">
              <Link href="/docs" className="hover:text-foreground transition-colors">
                {t("header.docs")}
              </Link>
              <span>&copy; {new Date().getFullYear()}</span>
            </nav>
          </div>
        </div>
      </footer>
    </div>
  );
}
