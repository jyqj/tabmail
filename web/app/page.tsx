"use client";

import { useState, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { SiteHeader } from "@/components/site-header";
import { TabMailLogo } from "@/components/tabmail-logo";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/lib/i18n";
import { listDomains, suggestAddress } from "@/lib/api";
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
  Loader2,
  Server,
  Inbox,
  Send,
  BookOpen,
  ChevronDown,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

function InboxMockup() {
  const { t } = useI18n();

  const emails = [
    {
      initials: "GH",
      color: "bg-teal-500/15 text-teal-700 dark:text-teal-300",
      sender: t("mock.sender1"),
      subject: t("mock.subject1"),
      time: t("mock.time1"),
      unread: true,
    },
    {
      initials: "ST",
      color: "bg-indigo-500/15 text-indigo-700 dark:text-indigo-300",
      sender: t("mock.sender2"),
      subject: t("mock.subject2"),
      time: t("mock.time2"),
      unread: true,
    },
    {
      initials: "TM",
      color: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
      sender: t("mock.sender3"),
      subject: t("mock.subject3"),
      time: t("mock.time3"),
      unread: false,
    },
  ];

  return (
    <div className="relative mt-20 mx-auto max-w-2xl">
      <div className="absolute -inset-px rounded-2xl bg-gradient-to-b from-teal-500/25 via-transparent to-indigo-500/20 blur-sm" />
      <div className="absolute -inset-4 bg-gradient-to-br from-teal-400/8 to-indigo-400/8 rounded-3xl blur-2xl" />
      <div className="relative rounded-2xl border border-border/60 bg-background/90 backdrop-blur-2xl shadow-2xl shadow-black/5 dark:shadow-black/30 overflow-hidden">
        <div className="flex items-center gap-2.5 border-b border-border/40 px-5 py-3 bg-muted/30">
          <div className="flex gap-1.5">
            <span className="h-2.5 w-2.5 rounded-full bg-rose-500/70" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-500/70" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-500/70" />
          </div>
          <div className="flex items-center gap-2 ml-3">
            <Inbox className="h-3 w-3 text-muted-foreground/60" />
            <code className="text-[10px] uppercase tracking-widest font-mono text-muted-foreground/60">
              {t("mock.inbox")} &mdash; test@yourdomain.com
            </code>
          </div>
        </div>
        <div className="divide-y divide-border/20">
          {emails.map((email) => (
            <div
              key={email.initials}
              className="flex items-center gap-4 px-5 py-3.5 hover:bg-muted/30 transition-colors cursor-default"
            >
              <div
                className={`h-9 w-9 rounded-lg flex items-center justify-center text-[11px] font-bold shrink-0 ${email.color}`}
              >
                {email.initials}
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center justify-between gap-2">
                  <span className={`text-sm ${email.unread ? "font-semibold text-foreground" : "text-muted-foreground"}`}>
                    {email.sender}
                  </span>
                  <span className="text-[10px] font-mono text-muted-foreground/60 shrink-0">
                    {email.time}
                  </span>
                </div>
                <p className={`text-[13px] truncate mt-0.5 ${email.unread ? "text-foreground/80" : "text-muted-foreground"}`}>
                  {email.subject}
                </p>
              </div>
              {email.unread && (
                <span className="h-1.5 w-1.5 rounded-full bg-teal-500 shrink-0" />
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
    <div className="border-b border-border/40 last:border-0">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between gap-4 py-6 text-left cursor-pointer group"
      >
        <span className="text-[15px] font-medium group-hover:text-teal-600 dark:group-hover:text-teal-400 transition-colors">{question}</span>
        <ChevronDown className={cn("h-4 w-4 text-muted-foreground shrink-0 transition-transform duration-300", open && "rotate-180")} />
      </button>
      <div className={cn(
        "grid transition-all duration-300 ease-out",
        open ? "grid-rows-[1fr] opacity-100 pb-6" : "grid-rows-[0fr] opacity-0"
      )}>
        <div className="overflow-hidden">
          <p className="text-sm text-muted-foreground leading-relaxed pr-8">
            {answer}
          </p>
        </div>
      </div>
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

  const [randomLoading, setRandomLoading] = useState(false);

  const handleRandom = async () => {
    setRandomLoading(true);
    try {
      const domains = await listDomains();
      const verified = (domains.data ?? []).filter((d) => d.is_verified && d.mx_verified);
      if (verified.length === 0) {
        toast.error(t("home.noDomains"));
        return;
      }
      const domain = verified[Math.floor(Math.random() * verified.length)];
      const res = await suggestAddress(domain.id, { subdomain: true });
      const addr = res.data.address;
      setAddress(addr);
      router.push(`/inbox/${encodeURIComponent(addr)}`);
    } catch {
      toast.error(t("home.randomFailed"));
    } finally {
      setRandomLoading(false);
    }
  };

  const FEATURES = [
    { icon: Globe, titleKey: "home.feat.domains", descKey: "home.feat.domainsDesc" },
    { icon: ShieldCheck, titleKey: "home.feat.access", descKey: "home.feat.accessDesc" },
    { icon: Clock, titleKey: "home.feat.cleanup", descKey: "home.feat.cleanupDesc" },
    { icon: Layers, titleKey: "home.feat.tenancy", descKey: "home.feat.tenancyDesc" },
    { icon: Zap, titleKey: "home.feat.perf", descKey: "home.feat.perfDesc" },
    { icon: Code2, titleKey: "home.feat.api", descKey: "home.feat.apiDesc" },
  ];

  const STEPS = [
    { step: "01", icon: Server, titleKey: "home.step01", descKey: "home.step01Desc" },
    { step: "02", icon: Send, titleKey: "home.step02", descKey: "home.step02Desc" },
    { step: "03", icon: Inbox, titleKey: "home.step03", descKey: "home.step03Desc" },
  ];

  return (
    <div className="flex min-h-screen flex-col selection:bg-teal-500/20 selection:text-foreground">
      <SiteHeader />

      <main className="flex-1">
        {/* Hero */}
        <section className="relative overflow-hidden pt-32 pb-16 md:pt-44 md:pb-24">
          {/* Gradient orbs */}
          <div className="absolute top-[-20%] left-[15%] w-[500px] h-[500px] bg-teal-400/[0.07] dark:bg-teal-400/[0.04] rounded-full blur-[100px]" />
          <div className="absolute top-[-10%] right-[10%] w-[400px] h-[400px] bg-indigo-400/[0.05] dark:bg-indigo-400/[0.03] rounded-full blur-[100px]" />
          <div className="absolute bottom-[10%] left-[50%] w-[300px] h-[300px] bg-amber-300/[0.04] dark:bg-amber-400/[0.02] rounded-full blur-[80px]" />

          {/* Dot grid */}
          <div className="absolute inset-0 -z-10 [mask-image:radial-gradient(ellipse_60%_50%_at_50%_0%,black_40%,transparent_100%)]">
            <div className="h-full w-full bg-[radial-gradient(circle,rgba(0,0,0,0.06)_1px,transparent_1px)] dark:bg-[radial-gradient(circle,rgba(255,255,255,0.04)_1px,transparent_1px)] bg-[size:24px_24px]" />
          </div>

          <div className="container mx-auto max-w-5xl px-4 text-center">
            <div className="animate-in fade-in slide-in-from-bottom-3 duration-600 inline-flex items-center gap-2.5 rounded-full border border-teal-500/20 bg-teal-500/[0.06] dark:bg-teal-500/[0.08] px-4 py-1.5 text-xs font-medium text-teal-700 dark:text-teal-300 mb-10">
              <span className="relative flex h-1.5 w-1.5">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-teal-500 opacity-75" />
                <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-teal-500" />
              </span>
              {t("home.badge")}
            </div>

            <h1 className="animate-in fade-in slide-in-from-bottom-4 duration-700 delay-100 text-4xl sm:text-5xl lg:text-7xl font-heading font-extrabold tracking-tight leading-[1.1]">
              {t("home.title1")}
              <br />
              <span className="bg-gradient-to-r from-teal-600 via-teal-500 to-cyan-500 dark:from-teal-400 dark:via-teal-300 dark:to-cyan-400 bg-clip-text text-transparent">
                {t("home.title2")}
              </span>
            </h1>

            <p className="animate-in fade-in slide-in-from-bottom-4 duration-700 delay-200 mx-auto mt-6 max-w-xl text-base md:text-lg text-muted-foreground leading-relaxed">
              {t("home.desc")}
            </p>

            <div className="animate-in fade-in slide-in-from-bottom-4 duration-700 delay-300 mx-auto mt-10 flex max-w-lg flex-col gap-3 sm:flex-row rounded-xl p-1.5 bg-muted/40 dark:bg-muted/20 border border-border/50 shadow-lg shadow-black/[0.03] dark:shadow-black/20">
              <div className="relative flex-1">
                <Mail className="absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground/50" />
                <Input
                  ref={heroInputRef}
                  className="h-12 pl-10 text-base border-0 bg-background shadow-sm rounded-lg focus-visible:ring-1 focus-visible:ring-teal-500/50"
                  placeholder={t("home.placeholder")}
                  value={address}
                  onChange={(e) => setAddress(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && go()}
                />
              </div>
              <Button
                className="h-12 px-6 gap-2 text-sm font-semibold rounded-lg bg-teal-600 hover:bg-teal-700 dark:bg-teal-500 dark:hover:bg-teal-600 text-white border-0"
                onClick={go}
                disabled={!address.trim()}
              >
                {t("home.openInbox")}
                <ArrowRight className="h-3.5 w-3.5" />
              </Button>
            </div>

            <button
              onClick={handleRandom}
              disabled={randomLoading}
              className="animate-in fade-in duration-700 delay-500 mt-5 inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-teal-600 dark:hover:text-teal-400 transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-wait"
            >
              {randomLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Shuffle className="h-3.5 w-3.5" />}
              {randomLoading ? t("home.generating") : t("home.random")}
            </button>

            <div className="animate-in fade-in slide-in-from-bottom-8 duration-1000 delay-500">
              <InboxMockup />
            </div>
          </div>
        </section>

        {/* How it works */}
        <section className="relative py-24 md:py-32">
          <div className="container mx-auto max-w-5xl px-4">
            <div className="text-center mb-16">
              <p className="text-xs font-mono font-semibold uppercase tracking-[0.2em] text-teal-600 dark:text-teal-400 mb-3">
                {t("home.howItWorks")}
              </p>
              <h2 className="text-2xl sm:text-3xl md:text-4xl font-heading font-bold tracking-tight">
                {t("home.threeSteps")}
              </h2>
            </div>

            <div className="grid gap-8 md:gap-6 md:grid-cols-3">
              {STEPS.map((s, i) => (
                <div key={s.step} className="relative group">
                  {i < STEPS.length - 1 && (
                    <div className="absolute top-10 left-[calc(50%+2.5rem)] hidden w-[calc(100%-5rem)] md:block">
                      <div className="border-t border-dashed border-teal-500/20 group-hover:border-teal-500/40 transition-colors" />
                    </div>
                  )}
                  <div className="flex flex-col items-center text-center">
                    <div className="relative flex h-20 w-20 items-center justify-center rounded-2xl bg-teal-500/[0.06] dark:bg-teal-500/[0.08] border border-teal-500/10 mb-5 group-hover:bg-teal-500/[0.1] group-hover:border-teal-500/20 transition-all duration-400">
                      <s.icon className="h-8 w-8 text-teal-600 dark:text-teal-400" />
                      <span className="absolute -top-2 -right-2 flex h-6 w-6 items-center justify-center rounded-full bg-teal-600 dark:bg-teal-500 text-[10px] font-bold text-white">
                        {s.step}
                      </span>
                    </div>
                    <h3 className="font-heading font-bold text-lg mb-2">{t(s.titleKey)}</h3>
                    <p className="text-sm text-muted-foreground leading-relaxed max-w-[280px]">
                      {t(s.descKey)}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* Features */}
        <section className="relative py-24 md:py-32 bg-muted/20 dark:bg-muted/5">
          <div className="container mx-auto max-w-5xl px-4">
            <div className="text-center mb-16">
              <p className="text-xs font-mono font-semibold uppercase tracking-[0.2em] text-teal-600 dark:text-teal-400 mb-3">
                {t("home.features")}
              </p>
              <h2 className="text-2xl sm:text-3xl md:text-4xl font-heading font-bold tracking-tight mb-4">
                {t("home.featuresTitle")}
              </h2>
              <p className="mx-auto max-w-lg text-muted-foreground">
                {t("home.featuresDesc")}
              </p>
            </div>

            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {FEATURES.map((f) => (
                <div
                  key={f.titleKey}
                  className="group relative rounded-xl border border-border/50 bg-background p-6 hover:border-teal-500/30 hover:shadow-lg hover:shadow-teal-500/[0.03] transition-all duration-400"
                >
                  <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-teal-500/[0.08] dark:bg-teal-500/[0.1] text-teal-600 dark:text-teal-400 mb-4 group-hover:bg-teal-500/[0.14] transition-colors">
                    <f.icon className="h-5 w-5" />
                  </div>
                  <h3 className="font-heading font-semibold text-[15px] mb-1.5">{t(f.titleKey)}</h3>
                  <p className="text-sm text-muted-foreground leading-relaxed">
                    {t(f.descKey)}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* CTA */}
        <section className="relative py-24 md:py-32 overflow-hidden">
          <div className="absolute inset-0 bg-gradient-to-br from-teal-600 via-teal-700 to-cyan-800 dark:from-teal-800 dark:via-teal-900 dark:to-cyan-950" />
          <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_20%,rgba(255,255,255,0.08),transparent_60%)]" />
          <div className="absolute inset-0 bg-[radial-gradient(circle_at_70%_80%,rgba(0,0,0,0.15),transparent_60%)]" />

          <div className="container mx-auto max-w-3xl px-4 text-center relative z-10">
            <h2 className="text-3xl md:text-4xl lg:text-5xl font-heading font-extrabold tracking-tight text-white mb-5">
              {t("home.ctaTitle")}
            </h2>
            <p className="text-lg text-white/70 max-w-xl mx-auto mb-10">
              {t("home.ctaDesc")}
            </p>
            <div className="flex flex-col sm:flex-row items-center justify-center gap-3">
              <Button
                size="lg"
                className="h-12 px-6 gap-2 text-sm font-semibold w-full sm:w-auto bg-white text-teal-700 hover:bg-white/90 border-0 shadow-lg"
                onClick={() => heroInputRef.current?.focus()}
              >
                <Mail className="h-4 w-4" />
                {t("home.ctaTry")}
              </Button>
              <Button
                variant="outline"
                size="lg"
                className="h-12 px-6 gap-2 text-sm font-semibold w-full sm:w-auto bg-transparent text-white border-white/20 hover:bg-white/10 hover:text-white"
                render={<Link href="/docs" />}
              >
                <BookOpen className="h-4 w-4" />
                {t("home.ctaDocs")}
              </Button>
            </div>
          </div>
        </section>

        {/* FAQ */}
        <section className="py-24 md:py-32">
          <div className="container mx-auto max-w-2xl px-4">
            <div className="text-center mb-14">
              <p className="text-xs font-mono font-semibold uppercase tracking-[0.2em] text-teal-600 dark:text-teal-400 mb-3">
                FAQ
              </p>
              <h2 className="text-2xl sm:text-3xl font-heading font-bold tracking-tight">
                {t("faq.title")}
              </h2>
            </div>
            <div>
              {(["1", "2", "3", "4", "5"] as const).map((n) => (
                <FAQItem key={n} question={t(`faq.q${n}`)} answer={t(`faq.a${n}`)} />
              ))}
            </div>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="border-t border-border/30">
        <div className="container mx-auto max-w-5xl px-4 py-8">
          <div className="flex flex-col md:flex-row items-center justify-between gap-4">
            <div className="flex items-center gap-2.5">
              <TabMailLogo size={26} />
              <span className="font-heading font-semibold">
                <span className="text-teal-600 dark:text-teal-400">Tab</span>Mail
              </span>
            </div>
            <nav className="flex items-center gap-5 text-sm text-muted-foreground">
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
