"use client";

// The landing page renders on the server; these are the only pieces that need
// the browser. Their copy arrives already translated so the markup the server
// sends and the markup React hydrates agree on the language.

import { useState, useCallback, useEffect, useRef, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { listOpenDomains, suggestOpenAddress } from "@/lib/api";
import { ArrowRight, ChevronDown, Code2, Loader2, Mail, RefreshCw, ShieldCheck } from "lucide-react";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

// The CTA button at the bottom of the page sends the reader back to the hero
// input, which lives in a different island.
export const ADDRESS_INPUT_ID = "home-address";

export interface AddressSearchLabels {
  placeholder: string;
  openInbox: string;
  random: string;
  generating: string;
  curlHint: string;
  noRegister: string;
  noDomains: string;
  randomFailed: string;
}

export function AddressSearch({ labels }: { labels: AddressSearchLabels }) {
  const router = useRouter();
  const [address, setAddress] = useState("");
  const [randomLoading, setRandomLoading] = useState(false);

  const go = useCallback(() => {
    const target = address.trim();
    if (!target) return;
    router.push(`/inbox/${encodeURIComponent(target)}`);
  }, [address, router]);

  const handleRandom = async () => {
    setRandomLoading(true);
    try {
      const domains = await listOpenDomains();
      const verified = (domains.data ?? []).filter((d) => d.is_verified && d.mx_verified);
      if (verified.length === 0) {
        toast.error(labels.noDomains);
        return;
      }
      const domain = verified[Math.floor(Math.random() * verified.length)];
      const res = await suggestOpenAddress(domain.id, { subdomain: domain.allow_random_subdomains });
      const addr = res.data.address;
      setAddress(addr);
      router.push(`/inbox/${encodeURIComponent(addr)}`);
    } catch {
      toast.error(labels.randomFailed);
    } finally {
      setRandomLoading(false);
    }
  };

  return (
    <>
      <div className="tm-reveal tm-reveal-4 flex items-center gap-2 mt-8 max-w-[540px]">
        <Input
          id={ADDRESS_INPUT_ID}
          className="h-[42px] text-sm flex-1 bg-card border-border rounded-lg focus-visible:ring-1 focus-visible:ring-primary/40 font-mono"
          placeholder={labels.placeholder}
          value={address}
          onChange={(e) => setAddress(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && go()}
        />
        <Button
          className="h-[42px] px-5 gap-2 text-sm font-medium rounded-lg"
          onClick={go}
          disabled={!address.trim()}
        >
          {labels.openInbox}
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
          {randomLoading ? labels.generating : labels.random}
        </button>
        <span className="flex items-center gap-1.5 opacity-60">
          <Code2 className="h-3 w-3" />
          {labels.curlHint}
        </span>
        <span className="flex items-center gap-1.5 opacity-60">
          <ShieldCheck className="h-3 w-3" />
          {labels.noRegister}
        </span>
      </div>
    </>
  );
}

// Reveals the server-rendered children as they scroll into view. The children
// stay Server Components; only the observer needs the browser.
export function ScrollReveal({ className, children }: { className?: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (typeof IntersectionObserver === "undefined") {
      el.querySelectorAll(".tm-fade-in").forEach((child) => child.classList.add("visible"));
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            entry.target.classList.add("visible");
            observer.unobserve(entry.target);
          }
        });
      },
      { threshold: 0.15, rootMargin: "0px 0px -40px 0px" }
    );
    el.querySelectorAll(".tm-fade-in").forEach((child) => observer.observe(child));
    return () => observer.disconnect();
  }, []);

  return <div ref={ref} className={className}>{children}</div>;
}

export function FaqItem({ question, answer }: { question: string; answer: string }) {
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

export function TryItButton({ label }: { label: string }) {
  return (
    <Button
      size="lg"
      className="h-11 px-6 gap-2 text-sm font-semibold w-full sm:w-auto bg-white text-primary hover:bg-white/90 border-0 shadow-lg shadow-black/10"
      onClick={() => {
        document.getElementById(ADDRESS_INPUT_ID)?.focus();
        window.scrollTo({ top: 0, behavior: "smooth" });
      }}
    >
      <Mail className="h-4 w-4" />
      {label}
    </Button>
  );
}
