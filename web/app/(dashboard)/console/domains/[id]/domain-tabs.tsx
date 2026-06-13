"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArrowLeft, Globe, Route } from "lucide-react";
import { useI18n } from "@/lib/i18n";
import { cn } from "@/lib/utils";

export function DomainTabs({ domainId }: { domainId: string }) {
  const { t } = useI18n();
  const pathname = usePathname();
  const base = `/console/domains/${domainId}`;
  const tabs = [
    { href: base, label: t("domainNav.dns"), icon: Globe },
    { href: `${base}/routes`, label: t("domainNav.routes"), icon: Route },
  ];

  return (
    <div className="border-b bg-background/95 px-4 py-3">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <Link
          href="/console/domains"
          className="inline-flex w-fit items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          {t("domainNav.back")}
        </Link>
        <nav aria-label={t("domainNav.aria")} className="flex gap-1 overflow-x-auto">
          {tabs.map((tab) => {
            const Icon = tab.icon;
            const active = pathname === tab.href;
            return (
              <Link
                key={tab.href}
                href={tab.href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                  active
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
                )}
              >
                <Icon className="h-3.5 w-3.5" />
                {tab.label}
              </Link>
            );
          })}
        </nav>
      </div>
    </div>
  );
}
