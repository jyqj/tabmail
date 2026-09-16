"use client";
import { useId, useState, useRef, type ReactNode } from "react";
import { toast } from "sonner";
import { useI18n } from "@/lib/i18n";
import { errorText } from "@/lib/company";
export function useText() {
  const { locale } = useI18n();
  return (zh: string, en: string) => (locale === "en" ? en : zh);
}
export function useAction() {
  const [busy, setBusy] = useState(false);
  const running = useRef(false);
  async function run(fn: () => Promise<void>) {
    if (running.current) return;
    running.current = true;
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      if (!(e instanceof DOMException && e.name === "AbortError"))
        toast.error(errorText(e));
    } finally {
      running.current = false;
      setBusy(false);
    }
  }
  return { busy, run };
}
export function Section({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <section className="rounded-xl border bg-card p-5 space-y-4">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children}
    </section>
  );
}
export function Field({
  label,
  children,
}: {
  label: string;
  children: (id: string) => ReactNode;
}) {
  const id = useId();
  return (
    <div className="space-y-1.5">
      <label className="text-sm font-medium" htmlFor={id}>
        {label}
      </label>
      {children(id)}
    </div>
  );
}
export const inputClass =
  "w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50";
export function ActionButton({
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      {...props}
      className={`rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted disabled:opacity-50 ${props.className ?? ""}`}
    >
      {children}
    </button>
  );
}
export function LoadError({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry: () => void;
}) {
  const t = useText();
  if (!error) return null;
  return (
    <div
      role="alert"
      className="rounded-md border border-destructive p-3 text-sm"
    >
      <p>{errorText(error)}</p>
      <ActionButton onClick={onRetry}>
        {t("重试加载", "Retry loading")}
      </ActionButton>
    </div>
  );
}
export function MailHTML({ html }: { html: string }) {
  return (
    <iframe
      title="HTML email preview"
      sandbox=""
      referrerPolicy="no-referrer"
      className="w-full min-h-72 rounded border bg-white"
      srcDoc={`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><base target="_blank">${html}`}
    />
  );
}
