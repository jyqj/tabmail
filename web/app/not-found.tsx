"use client";

import Link from "next/link";
import { useI18n } from "@/lib/i18n";
import { Button } from "@/components/ui/button";

export default function NotFoundPage() {
  const { t } = useI18n();

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <div className="w-full max-w-md rounded-xl bg-card p-8 text-center ring-1 ring-foreground/10">
        <p className="font-heading text-7xl font-bold tracking-tighter text-primary">
          404
        </p>
        <h1 className="mt-4 font-heading text-2xl font-bold tracking-tight text-foreground">
          {t("notFound.title")}
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("notFound.desc")}
        </p>
        <Button render={<Link href="/" />} className="mt-6">
          {t("notFound.home")}
        </Button>
      </div>
    </div>
  );
}
