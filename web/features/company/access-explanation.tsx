"use client";
import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { company } from "@/lib/company";
import type { AdminUser } from "@/lib/types";
import { EmployeeField } from "@/components/company/employee-field";
import { LoadError, Section, useText } from "@/components/company/common";
import type { AccessExplanation } from "./api";
export function AccessExplanationPanel({ mailbox, employees }: {
    mailbox: string;
    employees: AdminUser[];
}) {
    const t = useText();
    const [user, setUser] = useState("");
    const access = useAPI(user ? ["mailbox-access-explanation", mailbox, user] : null, () => company<AccessExplanation>(`/mailboxes/${mailbox}/access/${user}`));
    const reasons: Record<string, string> = { owner: t("邮箱所有权提供基础权限", "Mailbox ownership provides base rights"), grant: t("显式邮箱授权提供基础权限", "An explicit mailbox grant provides base rights"), none: t("没有所有权或显式授权；管理角色不等于正文权限", "No ownership or explicit grant; management is not content access"), inactive: t("账号已停用", "Account disabled"), expired: t("邮箱已过期", "Mailbox expired"), zone_restricted: t("权限配置限制了域名范围", "Permission profile excludes this domain"), profile_send_disabled: t("成员权限配置不允许发送", "Employee permission profile forbids sending"), sending_disabled: t("邮箱或公司策略暂停发送", "Company/mailbox policy pauses sending"), template_required: t("发送必须使用获准模板", "Sending requires an authorized template") };
    return <Section title={t("权限来源解释", "Explain effective access")}><EmployeeField label={t("查看成员权限", "Inspect employee access")} value={user} onChange={setUser} employees={employees}/><LoadError error={access.error} onRetry={() => void access.mutate()}/>{!access.error && access.data && <div className="space-y-2 text-sm"><p>{t("当前生效权限由服务器计算，下面不是另一份可编辑授权表。", "Effective rights are calculated by the server; this is not a second editable grant table.")}</p><p>{t("阅读", "Read")}: {String(access.data.can_read)} · {t("共享整理", "Organize")}: {String(access.data.can_organize)} · {t("代发", "Send as")}: {String(access.data.can_send)}</p>{access.data.reasons.map(reason => <p key={reason}>{reasons[reason] ?? reason}</p>)}</div>}</Section>;
}
