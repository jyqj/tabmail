"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useAuth } from "@/contexts/auth-context";
import { ThemeToggle } from "@/components/theme-toggle";
import { AuthDialog } from "@/components/auth-dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { healthCheck } from "@/lib/api";
import { Mail, LayoutDashboard, Shield, BookOpen, Activity } from "lucide-react";

export function SiteHeader() {
  const { level } = useAuth();
  const [healthy, setHealthy] = useState<boolean | null>(null);

  useEffect(() => {
    let cancelled = false;
    const check = () => {
      healthCheck()
        .then(() => {
          if (!cancelled) setHealthy(true);
        })
        .catch(() => {
          if (!cancelled) setHealthy(false);
        });
    };
    check();
    const timer = setInterval(check, 30_000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  return (
    <header className="sticky top-0 z-50 w-full border-b bg-background/80 backdrop-blur-sm">
      <div className="container flex h-14 items-center justify-between px-4 mx-auto max-w-6xl">
        <div className="flex items-center gap-6">
          <Link href="/" className="flex items-center gap-2 font-semibold tracking-tight">
            <Mail className="h-5 w-5 text-primary" />
            <span>TabMail</span>
          </Link>
          <nav className="hidden md:flex items-center gap-1">
            {(level === "tenant" || level === "admin") && (
              <Button variant="ghost" size="sm" render={<Link href="/console/domains" />} className="gap-1.5">
                <LayoutDashboard className="h-3.5 w-3.5" />
                Console
              </Button>
            )}
            {level === "mailbox" && (
              <Button variant="ghost" size="sm" render={<Link href="/" />} className="gap-1.5">
                <Mail className="h-3.5 w-3.5" />
                Mailbox Mode
              </Button>
            )}
            {level === "admin" && (
              <Button variant="ghost" size="sm" render={<Link href="/admin" />} className="gap-1.5">
                <Shield className="h-3.5 w-3.5" />
                Admin
              </Button>
            )}
            <Button variant="ghost" size="sm" render={<Link href="/docs" />} className="gap-1.5">
              <BookOpen className="h-3.5 w-3.5" />
              API Docs
            </Button>
          </nav>
        </div>
        <div className="flex items-center gap-2">
          <Badge
            variant="outline"
            className={
              healthy == null
                ? "hidden sm:inline-flex border-slate-500/20 text-slate-500"
                : healthy
                ? "hidden sm:inline-flex border-emerald-500/20 text-emerald-600"
                : "hidden sm:inline-flex border-rose-500/20 text-rose-600"
            }
          >
            <Activity className="mr-1 h-3 w-3" />
            {healthy == null ? "Checking" : healthy ? "API Healthy" : "API Down"}
          </Badge>
          <AuthDialog />
          <ThemeToggle />
        </div>
      </div>
    </header>
  );
}
