"use client";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { CodeCard, type CopyFn, type TFn } from "./shared";

export function QuickstartTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const curl = {
    health: `curl "$BASE_URL/health"`,
    token: `curl -X POST "$BASE_URL/api/v1/token" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "address": "secure@mail.example.com",
    "password": "Passw0rd!"
  }'`,
    domain: `curl -X POST "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "domain": "mail.example.com" }'`,
    deep: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "route_type": "deep_wildcard",
    "match_value": "**.mail.example.com",
    "auto_create_mailbox": true,
    "access_mode_default": "public"
  }'`,
  };
  return (
    <div className="grid gap-6 lg:grid-cols-[0.8fr_1.2fr]">
      <Card className="bg-background/90 shadow-sm">
        <CardHeader><CardTitle>{t("docs.authMatrix")}</CardTitle><CardDescription>{t("docs.authMatrixDesc")}</CardDescription></CardHeader>
        <CardContent className="space-y-3">
          {[["Public", "docs.publicDesc"], ["X-API-Key", "docs.apiKeyDesc"], ["JWT Admin", "docs.adminKeyDesc"], ["Bearer token", "docs.bearerDesc"]].map(([b, k]) => (
            <div key={b} className="rounded-xl border bg-background px-4 py-3"><div className="mb-1"><Badge variant="outline" className="font-mono text-[11px]">{b}</Badge></div><p className="text-sm leading-6 text-muted-foreground">{t(k)}</p></div>
          ))}
        </CardContent>
      </Card>
      <div className="grid gap-4">
        <CodeCard t={t} title={t("docs.health")} description={t("docs.healthDesc")} code={curl.health} onCopy={() => copy(curl.health, `${t("docs.health")} curl`)} />
        <CodeCard t={t} title={t("docs.mailboxTokenTitle")} description={t("docs.mailboxTokenDesc")} code={curl.token} onCopy={() => copy(curl.token, `${t("docs.mailboxTokenTitle")} curl`)} />
        <CodeCard t={t} title={t("docs.createDomainTitle")} description={t("docs.createDomainDesc")} code={curl.domain} onCopy={() => copy(curl.domain, `${t("docs.createDomainTitle")} curl`)} />
        <CodeCard t={t} title={t("docs.deepWildcardTitle")} description={t("docs.deepWildcardDesc")} code={curl.deep} onCopy={() => copy(curl.deep, `${t("docs.deepWildcardTitle")} curl`)} />
      </div>
    </div>
  );
}
