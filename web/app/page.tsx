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
  Loader2,
  Terminal,
  Copy,
  Link as LinkIcon,
  RefreshCw,
  BookOpen,
  ChevronDown,
  ArrowUpRight,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

function FAQItem({ question, answer }: { question: string; answer: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border-b border-border/40 last:border-0">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between gap-4 py-5 text-left cursor-pointer group"
      >
        <span className="text-[14px] font-medium group-hover:text-primary transition-colors">{question}</span>
        <ChevronDown className={cn("h-4 w-4 text-muted-foreground shrink-0 transition-transform duration-300", open && "rotate-180")} />
      </button>
      <div className={cn(
        "grid transition-all duration-300 ease-out",
        open ? "grid-rows-[1fr] opacity-100 pb-5" : "grid-rows-[0fr] opacity-0"
      )}>
        <div className="overflow-hidden">
          <p className="text-sm text-muted-foreground leading-relaxed pr-8">{answer}</p>
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
    { n: "01", title: t("home.step01"), desc: t("home.step01Desc") },
    { n: "02", title: t("home.step02"), desc: t("home.step02Desc") },
    { n: "03", title: t("home.step03"), desc: t("home.step03Desc") },
  ];

  return (
    <div className="flex min-h-screen flex-col selection:bg-primary/20 selection:text-foreground">
      <SiteHeader />

      <main className="flex-1">
        {/* Hero */}
        <section className="relative pt-16 pb-12 md:pt-24 md:pb-16 overflow-hidden">
          {/* Subtle radial glow */}
          <div className="absolute top-0 left-[20%] w-[600px] h-[400px] bg-primary/[0.04] rounded-full blur-[120px] pointer-events-none" />

          <div className="relative mx-auto max-w-[1180px] px-6 md:px-12">
            <div className="tm-reveal tm-reveal-1 flex items-center gap-2 mb-6">
              <span className="relative flex h-[6px] w-[6px]">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-75" />
                <span className="relative inline-flex rounded-full h-[6px] w-[6px] bg-emerald-500" />
              </span>
              <span className="font-mono text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                Self-hosted · v1.2 · API-first
              </span>
            </div>

            <h1 className="tm-reveal tm-reveal-2 text-5xl sm:text-6xl lg:text-[76px] font-bold leading-[1.02] tracking-[-0.04em] max-w-[900px]">
              {t("home.title1")}
              <br />
              <span className="text-primary">{t("home.title2")}</span>
            </h1>

            <p className="tm-reveal tm-reveal-3 mt-6 max-w-[540px] text-base leading-relaxed text-muted-foreground">
              {t("home.desc")}
            </p>

            <div className="tm-reveal tm-reveal-4 flex items-center gap-2 mt-8 max-w-[540px]">
              <Input
                ref={heroInputRef}
                className="h-[42px] text-sm flex-1 bg-card border-border rounded-lg focus-visible:ring-1 focus-visible:ring-primary/40 font-mono"
                placeholder={t("home.placeholder")}
                value={address}
                onChange={(e) => setAddress(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && go()}
              />
              <Button
                className="h-[42px] px-5 gap-2 text-sm font-medium rounded-lg"
                onClick={go}
                disabled={!address.trim()}
              >
                {t("home.openInbox")}
                <ArrowRight className="h-3.5 w-3.5" />
              </Button>
            </div>

            <div className="tm-reveal tm-reveal-5 flex items-center gap-5 mt-3.5 text-xs text-muted-foreground">
              <button
                onClick={handleRandom}
                disabled={randomLoading}
                className="flex items-center gap-1.5 hover:text-foreground transition-colors cursor-pointer disabled:opacity-50"
              >
                {randomLoading ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
                {randomLoading ? t("home.generating") : t("home.random")}
              </button>
              <span className="flex items-center gap-1.5 opacity-60">
                <Code2 className="h-3 w-3" />
                {t("home.curlHint") || "直接 curl"}
              </span>
              <span className="flex items-center gap-1.5 opacity-60">
                <ShieldCheck className="h-3 w-3" />
                {t("home.noRegister") || "不需要注册"}
              </span>
            </div>

          </div>
        </section>

        {/* How it works — code-first */}
        <section className="py-8 md:py-14">
          <div className="mx-auto max-w-[1180px] px-6 md:px-12">
            <div className="flex flex-col lg:flex-row gap-10 items-stretch">
              {/* Left: steps */}
              <div className="flex-1">
                <div className="font-mono text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground mb-2">
                  {t("home.howItWorks") || "三步上线"}
                </div>
                <h2 className="text-[28px] sm:text-[32px] font-semibold leading-tight tracking-[-0.025em]">
                  {t("home.threeSteps")}
                </h2>

                <ol className="mt-7 list-none p-0">
                  {STEPS.map((s) => (
                    <li key={s.n} className="flex gap-4 py-4 border-t border-border group">
                      <div className="font-mono text-[13px] font-semibold text-primary w-9 shrink-0 group-hover:translate-x-0.5 transition-transform">{s.n}</div>
                      <div className="flex-1 min-w-0">
                        <div className="text-[15px] font-semibold leading-snug">{s.title}</div>
                        <div className="text-[13px] text-muted-foreground mt-0.5 leading-relaxed">{s.desc}</div>
                      </div>
                      <ArrowRight className="h-3.5 w-3.5 text-muted-foreground/30 mt-1 shrink-0 group-hover:text-primary/50 group-hover:translate-x-0.5 transition-all" />
                    </li>
                  ))}
                </ol>
              </div>

              {/* Right: curl example */}
              <div className="flex-1 min-w-0">
                <div className="rounded-lg border border-border bg-card overflow-hidden shadow-sm">
                  <div className="flex items-center h-8 px-3 border-b border-border bg-muted/40 gap-2">
                    <Terminal className="h-3 w-3 text-muted-foreground/70" />
                    <span className="font-mono text-[11px] text-muted-foreground/70">{t("home.curlTitle") || "curl · 拉取最新邮件"}</span>
                    <div className="flex-1" />
                    <button className="p-1 rounded hover:bg-muted transition-colors" title="Copy">
                      <Copy className="h-3 w-3 text-muted-foreground/50" />
                    </button>
                  </div>
                  <pre className="font-mono text-[12px] leading-[1.6] bg-muted/20 border-0 px-4 py-4 overflow-auto text-foreground whitespace-pre">{`$ curl -H "X-API-Key: $TM_KEY" \\
       https://your-host/api/v1/mailbox/ops@mail.example.com
`}<span className="text-muted-foreground">{`{
  `}<span className="text-primary">{`"data"`}</span>{`: [
    {
      `}<span className="text-primary">{`"id"`}</span>{`:      "msg_3f7b…",
      `}<span className="text-primary">{`"sender"`}</span>{`:  "noreply@github.com",
      `}<span className="text-primary">{`"subject"`}</span>{`: "Pipeline #4129 passed",
      `}<span className="text-primary">{`"size"`}</span>{`:    `}<span className="text-amber-600 dark:text-amber-400">{`12442`}</span>{`,
      `}<span className="text-primary">{`"received_at"`}</span>{`: `}<span className="text-emerald-600 dark:text-emerald-400">{`"2026-05-19T12:01:14Z"`}</span>{`
    }
  ],
  `}<span className="text-primary">{`"meta"`}</span>{`: { "total": `}<span className="text-amber-600 dark:text-amber-400">{`142`}</span>{`, "page": `}<span className="text-amber-600 dark:text-amber-400">{`1`}</span>{`, "per_page": `}<span className="text-amber-600 dark:text-amber-400">{`20`}</span>{` }
}`}</span></pre>
                </div>
                <div className="flex items-center gap-2.5 mt-3">
                  <span className="inline-flex items-center gap-1.5 font-mono px-2 py-0.5 rounded border border-border bg-muted/40 text-[11px] text-muted-foreground hover:border-primary/30 hover:text-primary transition-colors cursor-pointer">
                    <Code2 className="h-2.5 w-2.5" /> openapi.yaml
                  </span>
                  <span className="inline-flex items-center gap-1.5 font-mono px-2 py-0.5 rounded border border-border bg-muted/40 text-[11px] text-muted-foreground hover:border-primary/30 hover:text-primary transition-colors cursor-pointer">
                    <LinkIcon className="h-2.5 w-2.5" /> /docs
                  </span>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* Features */}
        <section className="py-16 md:py-24">
          <div className="mx-auto max-w-[1180px] px-6 md:px-12">
            <div className="text-center mb-14">
              <p className="font-mono text-[10px] font-medium uppercase tracking-[0.14em] text-primary mb-3">
                {t("home.features")}
              </p>
              <h2 className="text-[26px] sm:text-[32px] font-semibold tracking-[-0.025em] mb-3">
                {t("home.featuresTitle")}
              </h2>
              <p className="mx-auto max-w-lg text-sm text-muted-foreground leading-relaxed">
                {t("home.featuresDesc")}
              </p>
            </div>

            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {FEATURES.map((f) => (
                <div
                  key={f.titleKey}
                  className="group relative rounded-lg border border-border bg-card p-5 hover:border-primary/30 hover:shadow-[0_2px_12px_-4px_hsl(174_100%_32%_/_0.08)] transition-all duration-300"
                >
                  <div className="flex h-9 w-9 items-center justify-center rounded-md bg-secondary text-primary mb-3 group-hover:bg-primary/10 transition-colors duration-300">
                    <f.icon className="h-[18px] w-[18px]" />
                  </div>
                  <h3 className="font-semibold text-[14px] mb-1 tracking-tight">{t(f.titleKey)}</h3>
                  <p className="text-[13px] text-muted-foreground leading-relaxed">
                    {t(f.descKey)}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* CTA */}
        <section className="relative py-16 md:py-24 overflow-hidden">
          <div className="absolute inset-0 bg-gradient-to-br from-primary via-primary to-emerald-800 dark:from-primary/90 dark:via-primary/80 dark:to-emerald-900" />
          <div className="absolute inset-0 opacity-[0.06]" style={{
            backgroundImage: "linear-gradient(to bottom, white 1px, transparent 1px), linear-gradient(to right, white 1px, transparent 1px)",
            backgroundSize: "48px 48px",
          }} />

          <div className="mx-auto max-w-3xl px-6 text-center relative z-10">
            <h2 className="text-3xl md:text-[40px] font-bold tracking-[-0.03em] text-white mb-4 leading-tight">
              {t("home.ctaTitle")}
            </h2>
            <p className="text-base text-white/65 max-w-xl mx-auto mb-8 leading-relaxed">
              {t("home.ctaDesc")}
            </p>
            <div className="flex flex-col sm:flex-row items-center justify-center gap-3">
              <Button
                size="lg"
                className="h-11 px-6 gap-2 text-sm font-semibold w-full sm:w-auto bg-white text-primary hover:bg-white/90 border-0 shadow-lg shadow-black/10"
                onClick={() => {
                  heroInputRef.current?.focus();
                  window.scrollTo({ top: 0, behavior: "smooth" });
                }}
              >
                <Mail className="h-4 w-4" />
                {t("home.ctaTry")}
              </Button>
              <Button
                variant="outline"
                size="lg"
                className="h-11 px-6 gap-2 text-sm font-semibold w-full sm:w-auto bg-transparent text-white border-white/20 hover:bg-white/10 hover:text-white"
                render={<Link href="/docs" />}
              >
                <BookOpen className="h-4 w-4" />
                {t("home.ctaDocs")}
              </Button>
            </div>
          </div>
        </section>

        {/* FAQ */}
        <section className="py-16 md:py-24">
          <div className="mx-auto max-w-2xl px-6">
            <div className="text-center mb-12">
              <p className="font-mono text-[10px] font-medium uppercase tracking-[0.14em] text-primary mb-3">
                FAQ
              </p>
              <h2 className="text-[24px] sm:text-[28px] font-semibold tracking-[-0.025em]">
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
      <footer className="border-t border-border">
        <div className="mx-auto max-w-[1180px] px-6 md:px-12 py-6">
          <div className="flex flex-col md:flex-row items-center justify-between gap-4">
            <div className="flex items-center gap-2.5">
              <TabMailLogo size={22} />
              <span className="font-semibold text-sm tracking-tight">
                <span className="text-primary">Tab</span>Mail
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
