"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { ActionButton, Field, inputClass, LoadError, Section, useAction, useText } from "@/components/company/common";
import { company, companyDomains, type CompanyDomain, type CompanySettings, type CompanySettingsInput } from "@/lib/company";
import { isConflict } from "@/lib/error-code";
import { sessionScope, useSessionScope } from "@/lib/session";
import { CompanyDomainsSection } from "@/components/company/domains";
import { CompanyMailSendPolicyField, sendPolicyDescription, sendPolicyLabel } from "@/components/company/send-policy";

const emptySettings: CompanySettingsInput = { name: "", primary_zone_id: "", revision: 0 };
type ReviewRequired = { minimumRevision: number; newerThan?: number; acknowledged?: boolean; uncertain?: boolean };

// A missing settings row is a real revision-zero snapshot. An unfinished,
// failed or malformed read must never be mistaken for that initial state.
function settingsSnapshot(value: unknown, tenant: string): CompanySettings | null | undefined {
    if (value === null) return null;
    if (!value || typeof value !== "object") return undefined;
    const settings = value as CompanySettings;
    if (settings.tenant_id !== tenant || typeof settings.name !== "string" || !settings.name.trim() ||
        typeof settings.primary_zone_id !== "string" || !settings.primary_zone_id ||
        typeof settings.domain !== "string" || !settings.domain || !Number.isSafeInteger(settings.revision) || settings.revision < 1 ||
        (settings.mail_send_policy !== undefined && !["free", "template_required", "disabled"].includes(settings.mail_send_policy))) return undefined;
    return settings;
}

function domainSnapshot(value: unknown): value is CompanyDomain[] {
    if (!Array.isArray(value)) return false;
    const ids = new Set<string>();
    return value.every(domain => {
        if (!domain || typeof domain.id !== "string" || !domain.id || ids.has(domain.id) ||
            typeof domain.domain !== "string" || !domain.domain || typeof domain.is_verified !== "boolean" ||
            typeof domain.mx_verified !== "boolean") return false;
        ids.add(domain.id);
        return true;
    });
}

export function DomainSettings() {
    const scope = useSessionScope();
    return <DomainSettingsSession key={scope} scope={scope} />;
}

function DomainSettingsSession({ scope }: { scope: string }) {
    const t = useText();
    const { busy, run } = useAction();
    const [tenant] = useState(() => typeof window === "undefined" ? "" : localStorage.getItem("tabmail_tenant_id") ?? "");
    const lifetime = useRef<object | null>(null);
    const draftIntent = useRef<object>({});
    const settings = useAPI("company-settings", () => company<CompanySettings | null>("/settings"));
    const zones = useAPI("company-domains", companyDomains);
    const [editing, setEditing] = useState<CompanySettingsInput | null>(null);
    const [reviewRequired, setReviewRequired] = useState<ReviewRequired | null>(null);
    const [reviewed, setReviewed] = useState<CompanySettings | null>(null);
    const [readError, setReadError] = useState<unknown>(null);
    const snapshot = settingsSnapshot(settings.data, tenant);
    const revision = snapshot?.revision ?? 0;
    const [highestRevision, setHighestRevision] = useState(0);
    if (snapshot !== undefined && revision > highestRevision) setHighestRevision(revision);
    const config = editing ?? snapshot ?? emptySettings;
    const observed = useRef({ highestRevision });
    useLayoutEffect(() => {
        lifetime.current = {};
        return () => { lifetime.current = null; };
    }, []);
    // Any committed cache generation, including a failed refresh or A → B → A
    // data change, retires an overlapping explicit review. Keep a revision
    // high-water mark even if a later cache response goes backwards.
    useLayoutEffect(() => { observed.current = { highestRevision }; }, [highestRevision,
        settings.data, settings.error, settings.isValidating, zones.data, zones.error, zones.isValidating]);
    const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();
    const domainData = zones.data;
    const domainsValid = domainSnapshot(domainData);
    const available = domainsValid ? domainData.filter(domain => domain.is_verified && domain.mx_verified) : [];
    const selectedAvailable = available.some(domain => domain.id === config.primary_zone_id);
    const stale = reviewRequired !== null || (editing !== null && editing.revision !== revision) || revision < highestRevision;
    const ready = snapshot !== undefined && revision >= highestRevision && domainsValid &&
        !settings.error && !settings.isLoading && !settings.isValidating && !zones.error && !zones.isLoading && !zones.isValidating;
    const settingsError = () => new Error(t("无法确认公司当前设置，请重新读取后核对。", "Could not confirm current company settings. Read and review again."));
    const domainsError = () => new Error(t("无法确认当前公司域名，请重新读取后核对。", "Could not confirm current company domains. Read and review again."));
    const loadError = readError || settings.error || zones.error ||
        (settings.data !== undefined && snapshot === undefined ? settingsError() : null) ||
        (zones.data !== undefined && !domainsValid ? domainsError() : null);
    const change = (next: CompanySettingsInput) => { draftIntent.current = {}; setEditing(next); };
    const reload = (forReview: boolean) => {
        const owner = lifetime.current;
        if (!owner || !owns(owner)) return;
        const intent = draftIntent.current;
        const generation = observed.current;
        const draft = config;
        return run(async () => {
            setReadError(null);
            try {
                // A bare SWR mutate can resolve to retained data after GET
                // failure. Read both resources explicitly before publishing
                // or granting permission to submit the retained draft again.
                const [settingsResult, domainsResult] = await Promise.all([
                    company<CompanySettings | null>("/settings"), companyDomains(),
                ]);
                if (!owns(owner)) return;
                if (generation !== observed.current || intent !== draftIntent.current) {
                    throw new Error(t("设置或草稿在读取期间发生变化，请重新核对。", "Settings or your draft changed while loading. Review again."));
                }
                const fresh = settingsSnapshot(settingsResult, tenant);
                const minimum = Math.max(generation.highestRevision, reviewRequired?.minimumRevision ?? draft.revision);
                const freshRevision = fresh?.revision ?? 0;
                if (fresh === undefined || freshRevision < minimum ||
                    (reviewRequired?.newerThan !== undefined && freshRevision <= reviewRequired.newerThan)) throw settingsError();
                if (!domainSnapshot(domainsResult)) throw domainsError();
                // This synchronous publication and draft adoption belong to
                // the checked intent. There is no automatic PUT or discarded
                // draft: the latest saved values remain visible for review.
                const published = Promise.all([
                    settings.mutate(fresh, { revalidate: false }), zones.mutate(domainsResult, { revalidate: false }),
                ]);
                if (forReview) {
                    setEditing({ ...draft, revision: freshRevision });
                    setReviewRequired(null);
                    setReviewed(fresh);
                }
                await published;
            } catch (error) {
                if (owns(owner)) setReadError(error);
            }
        });
    };
    const save = () => {
        const owner = lifetime.current;
        if (!owner || !owns(owner) || !ready || stale || !config.name.trim() || !selectedAvailable) return;
        const intent = draftIntent.current;
        const submitted = config;
        return run(async () => {
            setReadError(null);
            setReviewed(null);
            let result: CompanySettings;
            try {
                result = await company<CompanySettings>("/settings", { method: "PUT", body: {
                    name: submitted.name, primary_zone_id: submitted.primary_zone_id,
                    revision: submitted.revision, mail_send_policy: submitted.mail_send_policy,
                } satisfies CompanySettingsInput });
            } catch (error) {
                if (!owns(owner)) return;
                if (isConflict(error)) {
                    setEditing(current => current ?? submitted);
                    setReviewRequired({ minimumRevision: Math.max(submitted.revision, observed.current.highestRevision), newerThan: submitted.revision });
                } else if (error instanceof SyntaxError) {
                    // The JSON parser can fail after a successful HTTP status.
                    // The transport wraps non-2xx parse failures as API errors,
                    // so this is not a confirmed rejection to blindly retry.
                    // A real read may confirm either the old or a newer state;
                    // never invent a successful write or an incremented version.
                    setEditing(current => current ?? submitted);
                    setReviewRequired({ minimumRevision: Math.max(submitted.revision, observed.current.highestRevision), uncertain: true });
                    throw new Error(t("未能确认保存结果，请读取并核对后再试。", "Could not confirm the save result. Read and review before trying again."));
                }
                throw error;
            }
            if (!owns(owner)) return;
            toast.success(t("公司设置已保存", "Company settings saved"));
            const fresh = settingsSnapshot(result, tenant);
            const minimumRevision = Math.max(submitted.revision + 1, observed.current.highestRevision);
            if (!fresh || fresh.revision < minimumRevision) {
                setEditing(current => current ?? submitted);
                setReviewRequired({ minimumRevision, newerThan: submitted.revision, acknowledged: true });
                return;
            }
            if (intent === draftIntent.current) {
                setEditing(null);
                setReviewRequired(null);
            } else {
                setReviewRequired({ minimumRevision: fresh.revision });
            }
            // The PUT returns the persisted settings snapshot; publishing it
            // avoids losing a confirmed write behind an unsuccessful GET.
            await settings.mutate(fresh, { revalidate: false });
        });
    };
    return <div className="space-y-5">
      <CompanyDomainsSection />
      <Section title={t("公司主域名", "Company primary domain")}>
        <LoadError error={loadError} onRetry={() => { void reload(stale); }}/>
        {(settings.isLoading || settings.isValidating || zones.isLoading || zones.isValidating) &&
          <p role="status" className="text-sm text-muted-foreground">{t("正在读取公司设置与域名…", "Loading company settings and domains…")}</p>}
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("公司名称", "Company name")}>
            {(id) => (<input id={id} className={inputClass} maxLength={120} value={config.name} onChange={(e) => change({ ...config, name: e.target.value })}/>)}
          </Field>
          <Field label={t("已验证的主域名", "Verified primary domain")}>
            {(id) => (<select id={id} className={inputClass} value={config.primary_zone_id} onChange={(e) => change({ ...config, primary_zone_id: e.target.value })}>
                <option value="">{t("请选择", "Select")}</option>
                {config.primary_zone_id && !selectedAvailable && <option value={config.primary_zone_id} disabled>
                  {t("之前选定的域名（当前不可用）", "Previously selected domain (currently unavailable)")}
                </option>}
                {available.map((v) => (<option key={v.id} value={v.id}>
                      {v.domain}
                    </option>))}
              </select>)}
          </Field>
          <CompanyMailSendPolicyField value={config.mail_send_policy ?? "free"} onChange={(policy) => change({ ...config, mail_send_policy: policy })}/>
        </div>
        <p className="text-sm text-muted-foreground">
          {t("发送策略约束该公司所有邮箱的对外发送（管理员与属主同样受限）：", "The send policy governs outbound sending for every company mailbox (admins and owners included): ")}
          {t("自由撰写", "free-form writing")}
          {" = "}
          {sendPolicyDescription(t, "free")}
          {"; "}
          {t("仅限已发布模板", "templates only")}
          {" = "}
          {sendPolicyDescription(t, "template_required")}
          {"; "}
          {t("暂停发送", "sending disabled")}
          {" = "}
          {sendPolicyDescription(t, "disabled")}
          {"。"}
        </p>
        {stale && <div className="space-y-2">
          <p role="alert">{reviewRequired?.uncertain
            ? t("未能确认公司设置是否已保存。草稿已保留，请读取并核对后再试。", "Could not confirm whether the settings were saved. Your draft is preserved; read and review before trying again.")
            : reviewRequired?.acknowledged
            ? t("公司设置已保存，但未能确认当前状态。草稿已保留，请核对后再保存。", "The settings were saved, but their current state could not be confirmed. Your draft is preserved; review before saving again.")
            : t("公司设置已变化。草稿已保留，请读取最新设置和域名，核对后再保存。", "Company settings changed. Your draft is preserved. Read the latest settings and domains, review, then save again.")}</p>
          <ActionButton disabled={busy} onClick={() => reload(true)}>{t("核对最新设置", "Review latest settings")}</ActionButton>
        </div>}
        {reviewed && !stale && <div role="region" aria-label={t("最新已保存设置", "Latest saved settings")} className="space-y-2 rounded-md border p-3 text-sm">
          <p className="font-medium">{t("最新已保存设置", "Latest saved settings")}</p>
          <dl className="grid gap-1 md:grid-cols-[auto_1fr] md:gap-x-4">
            <dt>{t("公司名称", "Company name")}</dt><dd>{reviewed.name}</dd>
            <dt>{t("公司主域名", "Company primary domain")}</dt><dd>{reviewed.domain}</dd>
            <dt>{t("公司默认发送策略", "Company default send policy")}</dt><dd>{sendPolicyLabel(t, reviewed.mail_send_policy ?? "free")}</dd>
          </dl>
          <p>{t("请将以上设置与保留的草稿对照，确认后再次保存。", "Compare these saved settings with your preserved draft, then save when ready.")}</p>
        </div>}
        <ActionButton disabled={busy || !ready || stale || !config.name.trim() || !selectedAvailable} onClick={save}>
          {t("保存公司设置", "Save company settings")}
        </ActionButton>
      </Section>
    </div>;
}
