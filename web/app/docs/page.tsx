import { headers } from "next/headers";
import { SiteHeader } from "@/components/site-header";
import { getServerI18n } from "@/lib/i18n-server";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Shield, KeyRound, Mail, FileCode2, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { DocsEndpoints, DocsHeroActions, DocsTabs, DocsViewProvider, type DocsLinks } from "./docs-view";

// When the API lives behind its own host the browser talks to it directly;
// otherwise the Next rewrites under /backend-* proxy it, and the origin is
// whatever host served this request.
async function resolveApi(): Promise<{ origin: string; links: DocsLinks }> {
  const configured = (process.env.NEXT_PUBLIC_API_URL ?? "").replace(/\/+$/, "");
  let origin = configured;
  if (!origin) {
    const h = await headers();
    const host = h.get("x-forwarded-host") ?? h.get("host") ?? "";
    origin = host ? `${h.get("x-forwarded-proto") ?? "http"}://${host}` : "";
  }
  return {
    origin,
    links: {
      docs: configured ? "/docs" : "/backend-docs",
      redoc: configured ? "/redoc" : "/backend-redoc",
      openapi: "/openapi.yaml",
      health: "/health",
    },
  };
}

export default async function DocsPage() {
  const { t } = await getServerI18n();
  const { origin, links } = await resolveApi();

  return (
    <div className="flex min-h-screen flex-col bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.10),transparent_30%),linear-gradient(180deg,rgba(15,23,42,0.03),transparent_30%)]">
      <SiteHeader />
      <DocsViewProvider>
        <main className="flex-1">
          {/* Hero */}
          <section className="border-b bg-background/70">
            <div className="mx-auto grid max-w-7xl gap-6 px-4 py-10 lg:grid-cols-[1.3fr_0.7fr]">
              <div className="space-y-5">
                <div className="inline-flex items-center gap-2 rounded-full border bg-background px-3 py-1 text-xs text-muted-foreground shadow-sm">
                  <Sparkles className="h-3.5 w-3.5 text-primary" />
                  {t("docs.badge")}
                </div>
                <div className="space-y-3">
                  <h1 className="max-w-3xl text-4xl font-semibold tracking-tight sm:text-5xl">{t("docs.title")}</h1>
                  <p className="max-w-2xl text-base leading-7 text-muted-foreground">{t("docs.desc")}</p>
                </div>
                <DocsHeroActions
                  openapiHref={links.openapi}
                  labels={{
                    openSwagger: t("docs.openSwagger"),
                    switchRedoc: t("docs.switchRedoc"),
                    rawOpenapi: t("docs.rawOpenapi"),
                  }}
                />
                <div className="grid gap-3 sm:grid-cols-3">
                  <InfoCard icon={<Shield className="h-4 w-4 text-emerald-500" />} title={t("docs.admin")} description={t("docs.adminDesc")} />
                  <InfoCard icon={<KeyRound className="h-4 w-4 text-sky-500" />} title={t("docs.tenant")} description={t("docs.tenantDesc")} />
                  <InfoCard icon={<Mail className="h-4 w-4 text-amber-500" />} title={t("docs.mailbox")} description={t("docs.mailboxDesc")} />
                </div>
              </div>
              <Card className="border-primary/15 bg-[linear-gradient(180deg,rgba(99,102,241,0.10),transparent_55%),var(--card)] shadow-lg">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2"><FileCode2 className="h-4 w-4 text-primary" />{t("docs.endpoints")}</CardTitle>
                  <CardDescription>{t("docs.endpointsDesc")}</CardDescription>
                </CardHeader>
                <CardContent className="space-y-3">
                  <DocsEndpoints
                    origin={origin}
                    links={links}
                    labels={{
                      baseUrl: t("docs.baseUrl"),
                      swaggerUi: t("docs.swaggerUi"),
                      redoc: t("docs.redoc"),
                      openapi: t("docs.openapi"),
                      health: t("docs.health"),
                    }}
                  />
                </CardContent>
              </Card>
            </div>
          </section>

          {/* Tabs */}
          <section className="mx-auto max-w-7xl px-4 py-8">
            <DocsTabs
              origin={origin}
              links={links}
              labels={{
                swaggerUi: t("docs.swaggerUi"),
                redoc: t("docs.redoc"),
                quickstart: t("docs.quickstart"),
                deploy: t("guide.deploy"),
                domains: t("guide.domains"),
                api: t("guide.api"),
                ops: t("guide.ops"),
                liveRendered: t("docs.liveRendered"),
              }}
            />
          </section>
        </main>
      </DocsViewProvider>
    </div>
  );
}

function InfoCard({ icon, title, description }: { icon: ReactNode; title: string; description: string }) {
  return (
    <div className="rounded-2xl border bg-background/85 p-4 shadow-sm backdrop-blur">
      <div className="mb-3 flex h-9 w-9 items-center justify-center rounded-xl bg-muted">{icon}</div>
      <div className="space-y-1">
        <div className="font-medium">{title}</div>
        <p className="text-sm leading-6 text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}
