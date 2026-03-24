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
  Shield,
  BookOpen,
  Menu,
  Inbox,
} from "lucide-react";
import { TabMailLogo } from "@/components/tabmail-logo";

export function SiteHeader() {
  const { level } = useAuth();
  const { t } = useI18n();
  const isMobile = useIsMobile();
  const [healthy, setHealthy] = useState<boolean | null>(null);

  useEffect(() => {
    let cancelled = false;
    const check = () => {
      healthCheck()
        .then(() => { if (!cancelled) setHealthy(true); })
        .catch(() => { if (!cancelled) setHealthy(false); });
    };
    check();
    const timer = setInterval(check, 30_000);
    return () => { cancelled = true; clearInterval(timer); };
  }, []);

  const navItems = [
    ...(level === "tenant" || level === "admin"
      ? [{ href: "/console/domains", label: t("header.console"), icon: LayoutDashboard }]
      : []),
    ...(level === "mailbox"
      ? [{ href: "/", label: t("header.inbox"), icon: Inbox }]
      : []),
    ...(level === "admin"
      ? [{ href: "/admin", label: t("header.admin"), icon: Shield }]
      : []),
    { href: "/docs", label: t("header.docs"), icon: BookOpen },
  ];

  const statusDot = healthy == null
    ? "bg-slate-400"
    : healthy
      ? "bg-emerald-500 shadow-[0_0_6px_rgba(16,185,129,0.6)]"
      : "bg-rose-500 shadow-[0_0_6px_rgba(244,63,94,0.6)]";

  const statusText = healthy == null
    ? t("header.checking")
    : healthy ? t("header.healthy") : t("header.down");

  return (
    <header className="sticky top-0 z-50 w-full border-b bg-background/80 backdrop-blur-md supports-backdrop-filter:bg-background/60">
      <div className="container flex h-14 items-center justify-between px-4 mx-auto max-w-6xl">
        <div className="flex items-center gap-5">
          <Link href="/" className="flex items-center gap-2.5 group">
            <div className="transition-transform group-hover:scale-105">
              <TabMailLogo size={30} />
            </div>
            <span className="font-heading font-semibold tracking-tight text-[15px]">
              <span className="text-teal-600 dark:text-teal-400">Tab</span>Mail
            </span>
          </Link>

          {!isMobile && (
            <nav className="flex items-center gap-0.5">
              {navItems.map((item) => (
                <Button
                  key={item.href}
                  variant="ghost"
                  size="sm"
                  render={<Link href={item.href} />}
                  className="gap-1.5 text-muted-foreground hover:text-foreground"
                >
                  <item.icon className="h-3.5 w-3.5" />
                  {item.label}
                </Button>
              ))}
            </nav>
          )}
        </div>

        <div className="flex items-center gap-1.5">
          {!isMobile && (
            <div className="flex items-center gap-1.5 mr-1">
              <span className={`h-2 w-2 rounded-full ${statusDot} transition-colors`} />
              <span className="text-[11px] text-muted-foreground">{statusText}</span>
            </div>
          )}
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
                    <TabMailLogo size={28} />
                    <span className="font-heading">
                      <span className="text-teal-600 dark:text-teal-400">Tab</span>Mail
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
                          className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                        />
                      }
                    >
                      <item.icon className="h-4 w-4" />
                      {item.label}
                    </SheetClose>
                  ))}
                </nav>
                <div className="mt-auto px-4 pb-4">
                  <div className="flex items-center gap-2 rounded-lg bg-muted/50 px-3 py-2">
                    <span className={`h-2 w-2 rounded-full ${statusDot}`} />
                    <span className="text-xs text-muted-foreground">API {statusText}</span>
                  </div>
                </div>
              </SheetContent>
            </Sheet>
          )}
        </div>
      </div>
    </header>
  );
}
