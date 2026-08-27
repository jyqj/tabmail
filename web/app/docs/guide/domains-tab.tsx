"use client";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Globe, Database, Lock, Network } from "lucide-react";
import { CodeCard, NoteBox, SectionHeading, StepCard, SubSection, type CopyFn, type TFn } from "./shared";

export function DomainsTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const c = (code: string, label: string) => () => copy(code, label);
  const bindCmd = `curl -X POST "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "domain": "mail.example.com" }'`;
  const verifyCmd = `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/verify" \\
  -H "X-API-Key: $TENANT_API_KEY"`;
  const suggestCmd = `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/suggest-address" \\
  -H "X-API-Key: $TENANT_API_KEY"`;
  const suggestSubdomainCmd = `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/suggest-address?subdomain=true" \\
  -H "X-API-Key: $TENANT_API_KEY"`;
  const testSmtp = `nc 127.0.0.1 2525
EHLO localhost
MAIL FROM:<sender@test.com>
RCPT TO:<test@mail.example.com>
DATA
Subject: test
hello tabmail
.
QUIT`;

  return (
    <div className="space-y-10">
      <SectionHeading icon={<Globe className="h-6 w-6 text-primary" />} title={t("guide.domains.title")} desc={t("guide.domains.desc")} />

      <SubSection title={t("guide.domains.flowTitle")} desc={t("guide.domains.flowDesc")}>
        <div className="grid gap-3">
          {([1,2,3,4,5,6] as const).map(n => (
            <StepCard key={n} step={n} title={t(`guide.domains.step${n}`)} desc={t(`guide.domains.step${n}Desc`)}>
              {n === 2 && <div className="mt-3"><CodeCard t={t} title={t("guide.api.bindDomain")} description="" code={bindCmd} onCopy={c(bindCmd, "bind domain")} /></div>}
              {n === 4 && <div className="mt-3"><CodeCard t={t} title={t("guide.api.verifyDomain")} description="" code={verifyCmd} onCopy={c(verifyCmd, "verify")} /></div>}
              {n === 6 && <div className="mt-3"><CodeCard t={t} title="SMTP Test" description="" code={testSmtp} onCopy={c(testSmtp, "smtp test")} /></div>}
            </StepCard>
          ))}
        </div>
      </SubSection>

      <SubSection title={t("guide.domains.dnsTitle")} desc={t("guide.domains.dnsDesc")}>
        <div className="space-y-4">
          <Card className="bg-background/90"><CardHeader><CardTitle className="text-base flex items-center gap-2"><Database className="h-4 w-4 text-primary" />{t("guide.domains.dnsTxt")}</CardTitle><CardDescription>{t("guide.domains.dnsTxtDesc")}</CardDescription></CardHeader>
            <CardContent><pre className="rounded-xl bg-muted p-4 text-xs leading-6 overflow-x-auto"><code>{`mail.example.com.  IN  TXT  "tabmail-verify=<verification_token>"`}</code></pre></CardContent>
          </Card>
          <Card className="bg-background/90"><CardHeader><CardTitle className="text-base flex items-center gap-2"><Network className="h-4 w-4 text-primary" />{t("guide.domains.dnsMx")}</CardTitle><CardDescription>{t("guide.domains.dnsMxDesc")}</CardDescription></CardHeader>
            <CardContent><pre className="rounded-xl bg-muted p-4 text-xs leading-6 overflow-x-auto"><code>{`mail.example.com.  IN  MX  10  mail.example.com.`}</code></pre></CardContent>
          </Card>
          <Card className="bg-background/90"><CardHeader><CardTitle className="text-base flex items-center gap-2"><Lock className="h-4 w-4 text-primary" />{t("guide.domains.dnsSpf")}</CardTitle><CardDescription>{t("guide.domains.dnsSpfDesc")}</CardDescription></CardHeader>
            <CardContent><pre className="rounded-xl bg-muted p-4 text-xs leading-6 overflow-x-auto"><code>{`mail.example.com.  IN  TXT  "v=spf1 a mx -all"`}</code></pre></CardContent>
          </Card>
        </div>
      </SubSection>

      <SubSection title={t("guide.domains.routeTitle")} desc={t("guide.domains.routeDesc")}>
        <div className="grid gap-4 lg:grid-cols-2">
          {(["Exact", "Wildcard", "Deep", "Seq"] as const).map(r => {
            const key = r.toLowerCase() as "exact" | "wildcard" | "deep" | "seq";
            const colors: Record<string, string> = { exact: "border-emerald-500/30", wildcard: "border-sky-500/30", deep: "border-violet-500/30", seq: "border-amber-500/30" };
            return (
              <Card key={r} className={`bg-background/90 ${colors[key]}`}>
                <CardHeader><CardTitle className="text-base">{t(`guide.domains.route${r}`)}</CardTitle><CardDescription>{t(`guide.domains.route${r}Desc`)}</CardDescription></CardHeader>
                <CardContent><code className="block rounded-lg bg-muted p-3 text-xs">{t(`guide.domains.route${r}Example`)}</code></CardContent>
              </Card>
            );
          })}
        </div>
        <div className="mt-4"><NoteBox>{t("guide.domains.routePriority")}</NoteBox></div>
      </SubSection>

      <SubSection title={t("guide.domains.scenarioTitle")}>
        <div className="grid gap-4 lg:grid-cols-3">
          {([1,2,3] as const).map(n => (
            <Card key={n} className="bg-background/90"><CardHeader><CardTitle className="text-base">{t(`guide.domains.scenario${n}`)}</CardTitle></CardHeader>
              <CardContent><p className="text-sm text-muted-foreground">{t(`guide.domains.scenario${n}Desc`)}</p></CardContent>
            </Card>
          ))}
        </div>
      </SubSection>

      <SubSection title={t("guide.domains.randomTitle")} desc={t("guide.domains.randomDesc")}>
        <div className="space-y-4">
          <CodeCard t={t} title={t("guide.api.suggestAddress")} description="" code={suggestCmd} onCopy={c(suggestCmd, "suggest address")} />
          <NoteBox>{t("guide.domains.randomNote")}</NoteBox>
          <CodeCard t={t} title={t("guide.api.suggestSubdomainAddress")} description="" code={suggestSubdomainCmd} onCopy={c(suggestSubdomainCmd, "suggest subdomain address")} />
          <NoteBox>{t("guide.domains.randomSubdomainNote")}</NoteBox>
        </div>
      </SubSection>
    </div>
  );
}
