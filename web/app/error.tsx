"use client";

import { useEffect } from "react";
import { useI18n } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import { AlertTriangle } from "lucide-react";

export default function ErrorPage({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const { t } = useI18n();

  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <div className="w-full max-w-md rounded-xl bg-card p-8 text-center ring-1 ring-foreground/10">
        <div className="mx-auto mb-6 flex h-14 w-14 items-center justify-center rounded-full bg-destructive/10">
          <AlertTriangle className="h-7 w-7 text-destructive" />
        </div>
        <h1 className="font-heading text-2xl font-bold tracking-tight text-foreground">
          {t("error.title")}
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("error.desc")}
        </p>
        {error?.message && (
          <p className="mt-4 rounded-lg bg-muted/50 px-3 py-2 font-mono text-xs text-muted-foreground">
            {error.message}
          </p>
        )}
        <Button onClick={reset} className="mt-6">
          {t("error.retry")}
        </Button>
      </div>
    </div>
  );
}
