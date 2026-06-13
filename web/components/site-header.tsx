"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useAuth } from "@/contexts/auth-context";
import { useI18n } from "@/lib/i18n";
import { useIsMobile } from "@/hooks/use-mobile";
import { ThemeToggle } from "@/components/theme-toggle";
import { AuthDialog } from "@/components/auth-dialog";
import { SettingsPanel } from "@/components/settings-panel";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetTrigger,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetClose,
} from "@/components/ui/sheet";
import { healthCheck } from "@/lib/api";
import {
  LayoutDashboard,
  BookOpen,
  Menu,
  Inbox,
} from "lucide-react";
import { TabMailLogo } from "@/components/tabmail-logo";
import { cn } from "@/lib/utils";

export function SiteHeader() {
  const { level } = useAuth();
  const { t } = useI18n();
  const isMobile = useIsMobile();
  const [healthy, setHealthy] = useState<boolean | null>(null);
  const [latency, setLatency] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    const check = () => {
      const start = Date.now();
      healthCheck()
        .then(() => {
          if (!cancelled) {
            setHealthy(true);
            setLatency(Date.now() - start);
          }
        })
        .catch(() => { if (!cancelled) { setHealthy(false); setLatency(null); } });
    };
    check();
    const timer = setInterval(check, 30_000);
    return () => { cancelled = true; clearInterval(timer); };
  }, []);

  const navItems = [
    ...(level === "user" || level === "super_admin" || level === "admin"
      ? [{ href: "/console/domains", label: t("header.console"), icon: LayoutDashboard }]
      : []),
    ...(level === "mailbox"
      ? [{ href: "/", label: t("header.inbox"), icon: Inbox }]
      : []),
    { href: "/docs", label: t("header.docs"), icon: BookOpen },
  ];

  return (
    <header className="sticky top-0 z-50 w-full border-b border-border bg-card/85 backdrop-blur-md">
      <div className="flex h-[52px] items-center justify-between px-4 mx-auto max-w-[1280px]">
        <div className="flex items-center gap-3">
          <Link href="/" className="flex items-center gap-2.5 group">
            <TabMailLogo size={22} />
            <span className="font-semibold text-[15px] tracking-tight">
              <span className="text-primary">Tab</span>Mail
            </span>
          </Link>

          <div className="w-px h-[18px] bg-border mx-1" />

          {!isMobile && (
            <nav className="flex items-center gap-0.5">
              {navItems.map((item) => (
                <Button
                  key={item.href}
                  variant="ghost"
                  size="sm"
                  render={<Link href={item.href} />}
                  className="gap-1.5 text-muted-foreground hover:text-foreground text-[13px] font-medium h-8"
                >
                  {item.label}
                </Button>
              ))}
            </nav>
          )}
        </div>

        <div className="flex items-center gap-1.5">
          {!isMobile && <StatusPill healthy={healthy} latency={latency} />}
          <AuthDialog />
          <ThemeToggle />
          <SettingsPanel />

          {isMobile && (
            <Sheet>
              <SheetTrigger
                render={<Button variant="ghost" size="icon" className="h-8 w-8" />}
              >
                <Menu className="h-4 w-4" />
              </SheetTrigger>
              <SheetContent side="left" className="w-72">
                <SheetHeader>
                  <SheetTitle className="flex items-center gap-2.5">
                    <TabMailLogo size={22} />
                    <span className="font-semibold">
                      <span className="text-primary">Tab</span>Mail
                    </span>
                  </SheetTitle>
                  <SheetDescription>{t("header.nav")}</SheetDescription>
                </SheetHeader>
                <nav className="flex flex-col gap-1 px-4 pt-2">
                  {navItems.map((item) => (
                    <SheetClose
                      key={item.href}
                      render={
                        <Link
                          href={item.href}
                          className="flex items-center gap-3 rounded-md px-3 py-2.5 text-sm text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                        />
                      }
                    >
                      <item.icon className="h-4 w-4" />
                      {item.label}
                    </SheetClose>
                  ))}
                </nav>
                <div className="mt-auto px-4 pb-4">
                  <StatusPill healthy={healthy} latency={latency} />
                </div>
              </SheetContent>
            </Sheet>
          )}
        </div>
      </div>
    </header>
  );
}

function StatusPill({ healthy, latency }: { healthy: boolean | null; latency: number | null }) {
  const dotColor = healthy == null
    ? "bg-muted-foreground/40"
    : healthy
      ? "bg-emerald-500"
      : "bg-rose-500";

  return (
    <div className={cn(
      "inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5",
      "bg-muted/50"
    )}>
      <span className="relative flex h-[5px] w-[5px]">
        {healthy && (
          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-40" />
        )}
        <span className={cn("relative inline-flex rounded-full h-[5px] w-[5px] transition-colors", dotColor)} />
      </span>
      <span className="font-mono text-[10px] text-muted-foreground/70">
        {latency != null ? `${latency} ms` : "API"}
      </span>
    </div>
  );
}
