"use client";
import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { company } from "@/lib/company";
import type { AdminUser } from "@/lib/types";
import { sessionScope, useSessionScope } from "@/lib/session";
import { EmployeeField } from "@/components/company/employee-field";
import { ActionButton, LoadError, Section, useText } from "@/components/company/common";
import type { AccessExplanation } from "./api";

type AccessExplanationProps = {
  mailbox: string;
  employees: AdminUser[];
  employeesReady?: boolean;
  employeesError?: unknown;
};

function validExplanation(value: unknown, mailbox: string, user: string): value is AccessExplanation {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const result = value as AccessExplanation;
  return result.mailbox_id === mailbox && result.user_id === user &&
    ["owner", "grant", "none"].includes(result.source) &&
    ["free", "template_required", "disabled"].includes(result.send_policy) &&
    [result.active, result.can_read, result.can_organize, result.can_send, result.template_only].every(flag => typeof flag === "boolean") &&
    Array.isArray(result.reasons) && result.reasons.every(reason => typeof reason === "string");
}

export function AccessExplanationPanel(props: AccessExplanationProps) {
  const scope = useSessionScope();
  return <AccessExplanationSession key={`${scope}:${props.mailbox}`} {...props} scope={scope} />;
}

function AccessExplanationSession({ mailbox, employees, employeesReady = true, employeesError, scope }: AccessExplanationProps & { scope: string }) {
  const t = useText();
  const [tenant] = useState(() => typeof window === "undefined" ? "" : localStorage.getItem("tabmail_tenant_id") ?? "");
  const [user, setUser] = useState("");
  const [directory, setDirectory] = useState({ employees, generation: 0 });
  if (directory.employees !== employees) setDirectory({ employees, generation: directory.generation + 1 });
  // Inactive employees remain inspectable so the server can explain their
  // disabled state. A foreign or removed row cannot keep an old inspection.
  const available = employees.filter(employee => employee.tenant_id === tenant);
  const current = available.find(employee => employee.id === user);
  if (employeesReady && user && !current) setUser("");
  return <Section title={t("权限来源解释", "Explain effective access")}>
    <EmployeeField label={t("查看成员权限", "Inspect employee access")} value={user} employees={available} disabled={!employeesReady}
      onChange={value => {
        if (!employeesReady || scope !== sessionScope() || (value && !available.some(employee => employee.id === value))) return;
        setUser(value);
      }} />
    {!employeesReady ? <p role="status" className="text-sm text-muted-foreground">{employeesError
      ? t("成员列表暂不可用，请在页面上重试加载。", "Employee directory unavailable. Retry loading the page.")
      : t("正在读取当前成员列表…", "Loading current employee directory…")}</p>
      : !current ? <p className="text-sm text-muted-foreground">{available.length
        ? t("请选择成员以查看其当前权限。", "Select a member to inspect their current access.")
        : t("当前没有可查看权限的公司成员。", "No company members available to inspect.")}</p>
      : <AccessExplanationResult key={`${user}:${directory.generation}`} mailbox={mailbox} user={user} />}
  </Section>;
}

function AccessExplanationResult({ mailbox, user }: { mailbox: string; user: string }) {
  const t = useText();
  const access = useAPI(["mailbox-access-explanation", mailbox, user], async () => {
    const result = await company<unknown>(`/mailboxes/${encodeURIComponent(mailbox)}/access/${encodeURIComponent(user)}`);
    if (!validExplanation(result, mailbox, user)) throw new Error(t("无法确认该邮箱与成员的当前权限，请重新加载权限说明。", "Could not confirm current access for this mailbox and member. Reload the explanation."));
    return result;
  }, { revalidateOnMount: true, dedupingInterval: 0 });
  const loading = access.isLoading || access.isValidating || (!access.data && !access.error);
  const retry = () => { if (!loading) void access.mutate().catch(() => undefined); };
  const reasons: Record<string, string> = { owner: t("邮箱所有权提供基础权限", "Mailbox ownership provides base rights"), grant: t("显式邮箱授权提供基础权限", "An explicit mailbox grant provides base rights"), none: t("没有所有权或显式授权；管理角色不等于正文权限", "No ownership or explicit grant; management is not content access"), inactive: t("账号已停用", "Account disabled"), expired: t("邮箱已过期", "Mailbox expired"), zone_restricted: t("权限配置限制了域名范围", "Permission profile excludes this domain"), profile_send_disabled: t("成员权限配置不允许发送", "Employee permission profile forbids sending"), sending_disabled: t("邮箱或公司策略暂停发送", "Company/mailbox policy pauses sending"), template_required: t("发送必须使用获准模板", "Sending requires an authorized template") };
  return <>
    <LoadError error={access.error} onRetry={retry} />
    {loading && <p role="status" className="text-sm text-muted-foreground">{access.data
      ? t("正在刷新该成员的当前权限…", "Refreshing current employee access…")
      : t("正在读取该成员的当前权限…", "Loading current employee access…")}</p>}
    {!loading && !access.error && access.data && <div role="region" aria-label={t("当前成员权限", "Current employee access")} className="space-y-2 text-sm">
      <p>{t("当前生效权限由服务器计算，下面不是另一份可编辑授权表。", "Effective rights are calculated by the server; this is not a second editable grant table.")}</p>
      <p>{t("阅读", "Read")}: {String(access.data.can_read)} · {t("共享整理", "Organize")}: {String(access.data.can_organize)} · {t("代发", "Send as")}: {String(access.data.can_send)}</p>
      {access.data.reasons.map((reason, index) => <p key={`${index}:${reason}`}>{reasons[reason] ?? reason}</p>)}
    </div>}
    <ActionButton disabled={loading} onClick={retry}>{t("刷新权限说明", "Refresh access explanation")}</ActionButton>
  </>;
}
