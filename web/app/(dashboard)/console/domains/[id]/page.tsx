"use client";

import { useParams } from "next/navigation";
import { useCRUDPage } from "@/hooks/use-crud-page";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { getVerificationStatus, verifyDomain } from "@/lib/api";
import type { VerificationStatus } from "@/lib/types";
import {
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Globe,
  Mail,
  Shield,
  Key,
  RefreshCw,
} from "lucide-react";
import { toast } from "sonner";
import { useI18n } from "@/lib/i18n";
import { useState } from "react";
import { DomainTabs } from "./domain-tabs";

export default function DomainDetailPage() {
  const { t } = useI18n();
  const params = useParams();
  const domainId = params.id as string;

  const { data: response, isLoading: loading, mutate } = useCRUDPage(
    ["domain-dns", domainId],
    () => getVerificationStatus(domainId),
    "dns.loadFailed",
  );
  const status: VerificationStatus | undefined = response?.data;

  const [verifying, setVerifying] = useState(false);

  const handleVerify = async () => {
    setVerifying(true);
    try {
      await verifyDomain(domainId);
      toast.success(t("dns.verifyTriggered"));
      mutate();
    } catch {
      toast.error(t("dns.verifyFailed"));
    } finally {
      setVerifying(false);
    }
  };

  const checkIcon = (s: string) => {
    if (s === "pass") return <CheckCircle2 className="h-4 w-4 text-emerald-500" />;
    if (s === "fail") return <XCircle className="h-4 w-4 text-red-500" />;
    return <AlertTriangle className="h-4 w-4 text-amber-500" />;
  };

  const checkBadge = (s: string) => {
    if (s === "pass") return <Badge className="bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-400 text-xs">{t("dns.pass")}</Badge>;
    if (s === "fail") return <Badge className="bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400 text-xs">{t("dns.fail")}</Badge>;
    return <Badge variant="secondary" className="text-xs">{s}</Badge>;
  };

  const dnsChecks = status?.checks
    ? [
        { key: "TXT", label: t("dns.txtOwnership"), icon: <Globe className="h-4 w-4" />, check: status.checks.txt },
        { key: "MX", label: t("dns.mxRecord"), icon: <Mail className="h-4 w-4" />, check: status.checks.mx },
        { key: "SPF", label: t("dns.spfRecord"), icon: <Shield className="h-4 w-4" />, check: status.checks.spf },
        { key: "DKIM", label: t("dns.dkimRecord"), icon: <Key className="h-4 w-4" />, check: status.checks.dkim },
        { key: "DMARC", label: t("dns.dmarcRecord"), icon: <Shield className="h-4 w-4" />, check: status.checks.dmarc },
      ]
    : [];

  const passCount = dnsChecks.filter((c) => c.check.status === "pass").length;

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("dns.title")}
        description={t("dns.description")}
        actions={
          <Button
            size="sm"
            variant="outline"
            className="gap-1.5"
            onClick={handleVerify}
            disabled={verifying}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${verifying ? "animate-spin" : ""}`} />
            {t("dns.recheck")}
          </Button>
        }
      />
      <DomainTabs domainId={domainId} />

      <div className="p-4">
        {loading ? (
          <div className="space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        ) : !status ? (
          <Card>
            <CardContent className="py-12 text-center text-muted-foreground">
              <Globe className="h-10 w-10 mx-auto mb-3 opacity-30" />
              <p className="text-sm">{t("dns.noData")}</p>
            </CardContent>
          </Card>
        ) : (
          <div className="space-y-4">
            {/* Summary */}
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-base flex items-center gap-2">
                  {passCount === dnsChecks.length ? (
                    <CheckCircle2 className="h-5 w-5 text-emerald-500" />
                  ) : passCount >= 3 ? (
                    <AlertTriangle className="h-5 w-5 text-amber-500" />
                  ) : (
                    <XCircle className="h-5 w-5 text-red-500" />
                  )}
                  {t("dns.checklistTitle")}
                </CardTitle>
                <CardDescription>
                  {t("dns.checklistDesc", { pass: passCount, total: dnsChecks.length })}
                </CardDescription>
              </CardHeader>
              {status.txt_expected && (
                <CardContent className="pt-0">
                  <div className="text-xs text-muted-foreground space-y-1">
                    <p><span className="font-medium">TXT:</span> <code className="bg-muted px-1 py-0.5 rounded text-[11px]">{status.txt_expected}</code></p>
                    <p><span className="font-medium">MX:</span> <code className="bg-muted px-1 py-0.5 rounded text-[11px]">{status.expected_mx}</code></p>
                    {status.dkim_host && (
                      <p><span className="font-medium">DKIM:</span> <code className="bg-muted px-1 py-0.5 rounded text-[11px]">{status.dkim_host}</code></p>
                    )}
                  </div>
                </CardContent>
              )}
            </Card>

            {/* Individual checks */}
            {dnsChecks.map((item) => (
              <Card key={item.key}>
                <CardContent className="py-4">
                  <div className="flex items-start gap-3">
                    <div className="mt-0.5">{checkIcon(item.check.status)}</div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        {item.icon}
                        <span className="text-sm font-medium">{item.label}</span>
                        {checkBadge(item.check.status)}
                      </div>
                      {item.check.details && item.check.details.length > 0 && (
                        <div className="mt-2 space-y-1">
                          {item.check.details.map((d, i) => (
                            <p key={i} className="text-xs text-muted-foreground font-mono">{d}</p>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
