"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { ActionButton, Field, inputClass, LoadError, Section, useAction, useText } from "@/components/company/common";
import { company, companyDomains, type CompanySettings } from "@/lib/company";
import { CompanyDomainsSection } from "@/components/company/domains";
import { CompanyMailSendPolicyField, sendPolicyDescription } from "@/components/company/send-policy";
export function DomainSettings() {
    const t = useText();
    const { busy, run } = useAction();
    const settings = useAPI("company-settings", () => company<CompanySettings | null>("/settings"));
    const zones = useAPI("company-domains", companyDomains);
    const [editing, setEditing] = useState<CompanySettings | null>(null);
    const config = editing ?? settings.data ?? { name: "", primary_zone_id: "", revision: 0 };
    return <div className="space-y-5"><LoadError error={settings.error || zones.error} onRetry={() => { void settings.mutate(); void zones.mutate(); }}/>
      <CompanyDomainsSection />
      <Section title={t("公司主域名", "Company primary domain")}>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("公司名称", "Company name")}>
            {(id) => (<input id={id} className={inputClass} maxLength={120} value={config.name} onChange={(e) => setEditing({ ...config, name: e.target.value })}/>)}
          </Field>
          <Field label={t("已验证的主域名", "Verified primary domain")}>
            {(id) => (<select id={id} className={inputClass} value={config.primary_zone_id} onChange={(e) => setEditing({ ...config, primary_zone_id: e.target.value })}>
                <option value="">{t("请选择", "Select")}</option>
                {(zones.data ?? [])
                .filter((v) => v.is_verified && v.mx_verified)
                .map((v) => (<option key={v.id} value={v.id}>
                      {v.domain}
                    </option>))}
              </select>)}
          </Field>
          <CompanyMailSendPolicyField value={config.mail_send_policy ?? "free"} onChange={(policy) => setEditing({ ...config, mail_send_policy: policy })}/>
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
        <ActionButton disabled={busy || !config.name.trim() || !config.primary_zone_id} onClick={() => run(async () => {
            await company("/settings", { method: "PUT", body: config });
            await settings.mutate();
            setEditing(null);
            toast.success(t("公司设置已保存", "Company settings saved"));
        })}>
          {t("保存公司设置", "Save company settings")}
        </ActionButton>
      </Section>
    </div>;
}
