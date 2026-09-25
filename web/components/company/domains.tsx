"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import {
  addCompanyDomain,
  companyDomains,
  deleteCompanyDomain,
  verifyCompanyDomain,
  type CompanyDomain,
  type DomainDNSCheck,
  type DomainVerification,
} from "@/lib/company";
import { safeConfirm } from "@/lib/utils";
import {
  ActionButton,
  Field,
  inputClass,
  LoadError,
  Section,
  useAction,
  useText,
} from "@/components/company/common";

// In-place company domain onboarding: add a domain, publish its DNS records,
// verify and delete. Replaces the former dead link to /admin/domains, which
// manages platform zone infrastructure and has no tenant onboarding flow.
export function CompanyDomainsSection() {
  const t = useText();
  const { busy, run } = useAction();
  const domains = useAPI("company-domains", companyDomains);
  const [domain, setDomain] = useState("");
  const [expanded, setExpanded] = useState("");
  const [verifications, setVerifications] = useState<
    Record<string, DomainVerification>
  >({});
  return (
    <Section title={t("公司域名与 DNS 验证", "Company domains and DNS verification")}>
      <p className="text-sm text-muted-foreground">
        {t(
          "添加域名后在 DNS 服务商处发布对应记录，再点击“验证”。验证通过的域名才能被选为公司主域名。",
          "After adding a domain, publish its records at your DNS provider and click Verify. Only verified domains can become the company primary domain.",
        )}
      </p>
      <LoadError error={domains.error} onRetry={() => void domains.mutate()} />
      <div className="grid gap-4 md:grid-cols-[1fr_auto] md:items-end">
        <Field label={t("新增域名", "Add a domain")}>
          {(id) => (
            <input
              id={id}
              className={inputClass}
              placeholder="mail.example.com"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
            />
          )}
        </Field>
        <ActionButton
          disabled={busy || !domain.trim()}
          onClick={() =>
            run(async () => {
              await addCompanyDomain(domain.trim());
              setDomain("");
              await domains.mutate();
              toast.success(
                t(
                  "域名已添加，请发布 DNS 记录后验证",
                  "Domain added. Publish the DNS records, then verify",
                ),
              );
            })
          }
        >
          {t("添加域名", "Add domain")}
        </ActionButton>
      </div>
      {(domains.data ?? []).map((d) => (
        <div key={d.id} className="space-y-3 border-t pt-3 text-sm">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="font-medium break-all">{d.domain}</p>
              <p className="mt-1 flex flex-wrap gap-2">
                <StatusBadge
                  ok={d.is_verified}
                  okLabel={t("归属已验证", "Ownership verified")}
                  pendingLabel={t("归属未验证", "Ownership unverified")}
                />
                <StatusBadge
                  ok={d.mx_verified}
                  okLabel={t("MX 已验证", "MX verified")}
                  pendingLabel={t("MX 未验证", "MX unverified")}
                />
                <StatusBadge
                  ok={d.dkim_enabled}
                  okLabel={t("DKIM 已启用", "DKIM enabled")}
                  pendingLabel={t("DKIM 未启用", "DKIM disabled")}
                />
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <ActionButton
                onClick={() => setExpanded(expanded === d.id ? "" : d.id)}
              >
                {expanded === d.id
                  ? t("收起 DNS 记录", "Hide DNS records")
                  : t("查看 DNS 记录", "View DNS records")}
              </ActionButton>
              <ActionButton
                disabled={busy}
                onClick={() =>
                  run(async () => {
                    const v = await verifyCompanyDomain(d.id);
                    setVerifications((prev) => ({ ...prev, [d.id]: v }));
                    await domains.mutate();
                    toast.success(t("验证已执行", "Verification executed"));
                  })
                }
              >
                {t("验证", "Verify")}
              </ActionButton>
              <ActionButton
                disabled={busy}
                onClick={() =>
                  run(async () => {
                    if (
                      !safeConfirm(
                        t(
                          "只能删除未使用的域名。有邮箱、邮件或恢复记录的域名会受到保护，不会连带删除邮件。确认删除？",
                          "Only unused domains can be deleted. Mailboxes, messages and recovery records are protected; this does not delete mail. Continue?",
                        ),
                      )
                    )
                      return;
                    await deleteCompanyDomain(d.id);
                    setVerifications((prev) => {
                      const next = { ...prev };
                      delete next[d.id];
                      return next;
                    });
                    if (expanded === d.id) setExpanded("");
                    await domains.mutate();
                    toast.success(t("域名已删除", "Domain deleted"));
                  })
                }
              >
                {t("删除", "Delete")}
              </ActionButton>
            </div>
          </div>
          {expanded === d.id && <DnsRecords domain={d} />}
          {verifications[d.id] && <VerificationChecks v={verifications[d.id]} />}
        </div>
      ))}
      {!domains.isLoading && !(domains.data ?? []).length && (
        <p className="text-muted-foreground">{t("暂无域名", "No domains")}</p>
      )}
    </Section>
  );
}

function StatusBadge({
  ok,
  okLabel,
  pendingLabel,
}: {
  ok: boolean;
  okLabel: string;
  pendingLabel: string;
}) {
  return (
    <span
      className={
        ok
          ? "rounded-full border border-emerald-600/40 bg-emerald-600/10 px-2 py-0.5 text-xs text-emerald-700 dark:text-emerald-400"
          : "rounded-full border px-2 py-0.5 text-xs text-muted-foreground"
      }
    >
      {ok ? okLabel : pendingLabel}
    </span>
  );
}

function RecordRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-1 md:grid-cols-[10rem_1fr] md:items-center">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <code className="break-all rounded bg-background px-2 py-1 text-xs">
        {value}
      </code>
    </div>
  );
}

function DnsRecords({ domain }: { domain: CompanyDomain }) {
  const t = useText();
  return (
    <div className="space-y-2 rounded border bg-muted/40 p-3">
      <p className="text-xs text-muted-foreground">
        {t(
          "在域名 DNS 服务商处发布以下记录，生效后点击“验证”。",
          "Publish the records below at your DNS provider, then click Verify once they take effect.",
        )}
      </p>
      <RecordRow
        label={t("归属验证 TXT", "Ownership TXT")}
        value={domain.txt_record}
      />
      <RecordRow label={t("MX 记录", "MX record")} value={domain.expected_mx} />
      {domain.dkim_host && domain.dkim_record ? (
        <>
          <RecordRow label={t("DKIM 主机", "DKIM host")} value={domain.dkim_host} />
          <RecordRow
            label={t("DKIM 记录", "DKIM record")}
            value={domain.dkim_record}
          />
        </>
      ) : (
        <p className="text-xs text-muted-foreground">
          {t(
            "DKIM 记录暂未提供：完成归属验证后由系统生成。",
            "No DKIM record yet: it is generated after ownership verification completes.",
          )}
        </p>
      )}
    </div>
  );
}

function VerificationChecks({ v }: { v: DomainVerification }) {
  const t = useText();
  const labels: Record<keyof DomainVerification["checks"], string> = {
    txt: t("归属 TXT", "Ownership TXT"),
    mx: t("MX", "MX"),
    spf: t("SPF", "SPF"),
    dkim: t("DKIM", "DKIM"),
    dmarc: t("DMARC", "DMARC"),
  };
  return (
    <div className="space-y-2 rounded border p-3">
      <p className="font-medium">{t("验证结果", "Verification results")}</p>
      {(
        Object.entries(v.checks) as [keyof DomainVerification["checks"], DomainDNSCheck][]
      ).map(([name, check]) => (
        <div key={name}>
          <p className="flex flex-wrap items-center gap-2">
            <StatusBadge
              ok={check.status === "pass"}
              okLabel={t("通过", "pass")}
              pendingLabel={check.status}
            />
            <span className="font-medium">{labels[name]}</span>
          </p>
          {check.details?.length ? (
            <ul className="mt-1 list-disc pl-5 text-xs text-muted-foreground">
              {check.details.map((line, i) => (
                <li key={i} className="break-all">
                  {line}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ))}
    </div>
  );
}
