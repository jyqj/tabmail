"use client";

import { useState } from "react";
import { Terminal } from "lucide-react";
import { CodeCard, SectionHeading, SubSection, type CopyFn, type TFn } from "./shared";

export function ApiTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const c = (code: string, label: string) => () => copy(code, label);
  const [activeSec, setActiveSec] = useState("setup");
  const setup = `export BASE_URL='http://127.0.0.1:8080'\nexport ADMIN_ACCESS_TOKEN='<admin-jwt-access-token>'`;
  const cmds: Record<string, string> = {
    createPlan: `curl -X POST "$BASE_URL/api/v1/admin/plans" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "name": "starter", "max_domains": 5,
    "max_mailboxes_per_domain": 200, "max_messages_per_mailbox": 500,
    "max_message_bytes": 10485760, "retention_hours": 24,
    "rpm_limit": 120, "daily_quota": 20000
  }'`,
    createTenant: `curl -X POST "$BASE_URL/api/v1/admin/tenants" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" \\
  -H 'Content-Type: application/json' \\
  -d '{ "name": "tenant-a", "plan_id": "<plan-id>" }'`,
    createApiKey: `curl -X POST "$BASE_URL/api/v1/admin/tenants/$TENANT_ID/keys" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" \\
  -H 'Content-Type: application/json' \\
  -d '{ "label": "default key", "scopes": ["domains:read","domains:write","routes:read","routes:write","mailboxes:read","mailboxes:write","messages:read","messages:write"] }'`,
    bindDomain: `curl -X POST "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "domain": "mail.example.com" }'`,
    listDomains: `curl "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    verifyDomain: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/verify" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    checkVerification: `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/verification-status" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    suggestAddress: `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/suggest-address" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    suggestSubdomainAddress: `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/suggest-address?subdomain=true" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    createWildcard: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "route_type": "wildcard",
    "match_value": "*.mail.example.com",
    "auto_create_mailbox": true,
    "access_mode_default": "public"
  }'`,
    createSequence: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "route_type": "sequence",
    "match_value": "box-{n}.mail.example.com",
    "range_start": 1, "range_end": 1000,
    "auto_create_mailbox": true,
    "access_mode_default": "token"
  }'`,
    createDeepWildcard: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "route_type": "deep_wildcard",
    "match_value": "**.mail.example.com",
    "auto_create_mailbox": true,
    "access_mode_default": "public"
  }'`,
    listRoutes: `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    createPublicMailbox: `curl -X POST "$BASE_URL/api/v1/mailboxes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "address": "demo@mail.example.com", "access_mode": "public" }'`,
    createTokenMailbox: `curl -X POST "$BASE_URL/api/v1/mailboxes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "address": "secure@mail.example.com",
    "password": "Passw0rd!",
    "access_mode": "token"
  }'`,
    getToken: `curl -X POST "$BASE_URL/api/v1/token" \\
  -H 'Content-Type: application/json' \\
  -d '{ "address": "secure@mail.example.com", "password": "Passw0rd!" }'`,
    listMailboxes: `curl "$BASE_URL/api/v1/mailboxes" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    listMessages: `curl "$BASE_URL/api/v1/mailbox/demo@mail.example.com"`,
    listMessagesByToken: `curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    listMessagesByApiKey: `curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    viewMessage: `curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    viewSource: `curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID/source" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    markRead: `curl -X PATCH "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    deleteMessage: `curl -X DELETE "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    purgeMailbox: `curl -X DELETE "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    systemStats: `curl "$BASE_URL/api/v1/admin/stats" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN"`,
    monitorHistory: `curl "$BASE_URL/api/v1/admin/monitor/history?page=1&per_page=20&type=message" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN"`,
    monitorSse: `curl -N "$BASE_URL/api/v1/admin/monitor/events" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN"`,
    getPolicy: `curl "$BASE_URL/api/v1/admin/policy" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN"`,
    updatePolicy: `curl -X PATCH "$BASE_URL/api/v1/admin/policy" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "default_accept": true,
    "accept_domains": [],
    "reject_domains": ["blocked.example.com", "*.trash.test"],
    "default_store": true,
    "store_domains": [],
    "discard_domains": ["devnull.example.com"],
    "reject_origin_domains": ["*.spam.test"]
  }'`,
    impersonate: `curl "$BASE_URL/api/v1/domains" \\
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" \\
  -H "X-Tenant-ID: $TENANT_ID"`,
    smtpTest: `nc 127.0.0.1 2525\nEHLO localhost\nMAIL FROM:<sender@example.org>\nRCPT TO:<demo@mail.example.com>\nDATA\nSubject: hello\nFrom: sender@example.org\nTo: demo@mail.example.com\n\nhello tabmail\n.\nQUIT`,
    smtpTlsTest: `openssl s_client -starttls smtp -crlf -connect 127.0.0.1:2525`,
  };

  type Section = { id: string; titleKey: string; descKey: string; sidebarKey: string; items: { key: string; cmdKey: string; note?: string }[] };
  const sections: Section[] = [
    { id: "s1", sidebarKey: "guide.api.sidebarS1", titleKey: "guide.api.s1Title", descKey: "guide.api.s1Desc", items: [
      { key: "guide.api.createPlan", cmdKey: "createPlan" },
      { key: "guide.api.createTenant", cmdKey: "createTenant" },
      { key: "guide.api.createApiKey", cmdKey: "createApiKey", note: "guide.api.createApiKeyNote" },
    ]},
    { id: "s2", sidebarKey: "guide.api.sidebarS2", titleKey: "guide.api.s2Title", descKey: "guide.api.s2Desc", items: [
      { key: "guide.api.bindDomain", cmdKey: "bindDomain" },
      { key: "guide.api.listDomains", cmdKey: "listDomains" },
      { key: "guide.api.verifyDomain", cmdKey: "verifyDomain" },
      { key: "guide.api.checkVerification", cmdKey: "checkVerification" },
      { key: "guide.api.suggestAddress", cmdKey: "suggestAddress" },
      { key: "guide.api.suggestSubdomainAddress", cmdKey: "suggestSubdomainAddress" },
      { key: "guide.api.createWildcard", cmdKey: "createWildcard" },
      { key: "guide.api.createSequence", cmdKey: "createSequence" },
      { key: "guide.api.createDeepWildcard", cmdKey: "createDeepWildcard" },
      { key: "guide.api.listRoutes", cmdKey: "listRoutes" },
    ]},
    { id: "s3", sidebarKey: "guide.api.sidebarS3", titleKey: "guide.api.s3Title", descKey: "guide.api.s3Desc", items: [
      { key: "guide.api.createPublicMailbox", cmdKey: "createPublicMailbox" },
      { key: "guide.api.createTokenMailbox", cmdKey: "createTokenMailbox" },
      { key: "guide.api.getToken", cmdKey: "getToken" },
      { key: "guide.api.listMailboxes", cmdKey: "listMailboxes" },
      { key: "guide.api.listMessages", cmdKey: "listMessages" },
      { key: "guide.api.listMessagesByToken", cmdKey: "listMessagesByToken" },
      { key: "guide.api.listMessagesByApiKey", cmdKey: "listMessagesByApiKey" },
      { key: "guide.api.viewMessage", cmdKey: "viewMessage" },
      { key: "guide.api.viewSource", cmdKey: "viewSource" },
      { key: "guide.api.markRead", cmdKey: "markRead" },
      { key: "guide.api.deleteMessage", cmdKey: "deleteMessage" },
      { key: "guide.api.purgeMailbox", cmdKey: "purgeMailbox" },
    ]},
    { id: "s4", sidebarKey: "guide.api.sidebarS4", titleKey: "guide.api.s4Title", descKey: "guide.api.s4Desc", items: [
      { key: "guide.api.systemStats", cmdKey: "systemStats" },
      { key: "guide.api.monitorHistory", cmdKey: "monitorHistory" },
      { key: "guide.api.monitorSse", cmdKey: "monitorSse" },
      { key: "guide.api.getPolicy", cmdKey: "getPolicy" },
      { key: "guide.api.updatePolicy", cmdKey: "updatePolicy" },
      { key: "guide.api.impersonate", cmdKey: "impersonate" },
    ]},
    { id: "s5", sidebarKey: "guide.api.sidebarS5", titleKey: "guide.api.s5Title", descKey: "guide.api.s5Desc", items: [
      { key: "guide.api.smtpTest", cmdKey: "smtpTest" },
      { key: "guide.api.smtpTlsTest", cmdKey: "smtpTlsTest" },
    ]},
  ];

  const scrollTo = (id: string) => {
    setActiveSec(id);
    document.getElementById(`api-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="space-y-10">
      <SectionHeading icon={<Terminal className="h-6 w-6 text-primary" />} title={t("guide.api.title")} desc={t("guide.api.desc")} />
      <div className="grid gap-8 lg:grid-cols-[200px_1fr]">
        {/* Sidebar nav */}
        <nav className="hidden lg:block sticky top-24 self-start space-y-1">
          <button onClick={() => scrollTo("setup")} className={`w-full text-left rounded-lg px-3 py-2 text-sm transition-colors ${activeSec === "setup" ? "bg-primary/10 text-primary font-medium" : "text-muted-foreground hover:text-foreground hover:bg-muted/50"}`}>{t("guide.api.sidebarSetup")}</button>
          {sections.map(s => (
            <button key={s.id} onClick={() => scrollTo(s.id)} className={`w-full text-left rounded-lg px-3 py-2 text-sm transition-colors ${activeSec === s.id ? "bg-primary/10 text-primary font-medium" : "text-muted-foreground hover:text-foreground hover:bg-muted/50"}`}>{t(s.sidebarKey)}</button>
          ))}
        </nav>
        {/* Content */}
        <div className="space-y-10 min-w-0">
          <div id="api-setup"><CodeCard t={t} title={t("guide.api.setupTitle")} description={t("guide.api.setupDesc")} code={setup} onCopy={c(setup, "setup")} /></div>
          {sections.map(s => (
            <div key={s.id} id={`api-${s.id}`}>
              <SubSection title={t(s.titleKey)} desc={t(s.descKey)}>
                <div className="grid gap-4">{s.items.map(item => (
                  <div key={item.cmdKey}>
                    <CodeCard t={t} title={t(item.key)} description={item.note ? t(item.note) : ""} code={cmds[item.cmdKey]} onCopy={c(cmds[item.cmdKey], t(item.key))} />
                  </div>
                ))}</div>
              </SubSection>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
