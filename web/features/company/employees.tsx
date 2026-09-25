"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { ActionButton, Field, inputClass, LoadError, Section, useAction, useText } from "@/components/company/common";
import Link from "next/link";
import { company, allEmployees, type CompanySettings, type Invitation } from "@/lib/company";
import { OffboardingPanel } from "./offboarding-panel";
export function Employees() {
    const t = useText();
    const { busy, run } = useAction();
    const settings = useAPI("company-settings", () => company<CompanySettings | null>("/settings"));
    const members = useAPI("company-employees", allEmployees);
    const invitations = useAPI("company-invitations", () => company<Invitation[]>("/invitations"));
    const [email, setEmail] = useState("");
    const [name, setName] = useState("");
    const [local, setLocal] = useState("");
    const [activation, setActivation] = useState("");
    return <div className="space-y-5"><LoadError error={members.error || invitations.error || settings.error} onRetry={() => { void members.mutate(); void invitations.mutate(); void settings.mutate(); }}/>
      <Section title={t("邀请员工并分配个人邮箱", "Invite an employee and assign a personal mailbox")}>
        <p className="text-sm text-muted-foreground">
          {t("激活链接有效 72 小时，只可使用一次。请通过可信渠道交付；此操作不会自动发送外部邮件。", "Activation links expire after 72 hours and are single-use. Deliver through a trusted channel; this action does not send external mail.")}
        </p>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("登录邮箱", "Login email")}>
            {(id) => (<input id={id} className={inputClass} type="email" value={email} onChange={(e) => setEmail(e.target.value)}/>)}
          </Field>
          <Field label={t("姓名", "Display name")}>
            {(id) => (<input id={id} className={inputClass} value={name} onChange={(e) => setName(e.target.value)}/>)}
          </Field>
          <Field label={t("公司邮箱用户名", "Company mailbox local part")}>
            {(id) => (<input id={id} className={inputClass} value={local} placeholder={`alice @ ${settings.data?.domain ?? "company"}`} onChange={(e) => setLocal(e.target.value)}/>)}
          </Field>
        </div>
        <ActionButton disabled={busy || !email || !local || !name || !settings.data} onClick={() => run(async () => {
            const v = await company<{
                activation_token: string;
            }>("/invitations", {
                method: "POST",
                body: { email, display_name: name, local_part: local },
            });
            setActivation(`${window.location.origin}/auth/activate#${v.activation_token}`);
            setEmail("");
            setName("");
            setLocal("");
            await invitations.mutate();
            toast.success(t("邀请已生成", "Invitation created"));
        })}>
          {t("生成员工邀请", "Create employee invitation")}
        </ActionButton>
        {activation && (<div className="rounded border p-3 space-y-2">
            <Field label={t("一次性激活链接（离开页面后不再显示）", "One-time activation link (not retained after leaving)")}>
              {(id) => (<input id={id} className={inputClass} readOnly value={activation}/>)}
            </Field>
            <ActionButton onClick={() => run(async () => {
                await navigator.clipboard.writeText(activation);
                toast.success(t("已复制", "Copied"));
            })}>
              {t("复制激活链接", "Copy activation link")}
            </ActionButton>
          </div>)}
        {(invitations.data ?? []).map((v) => (<div key={v.id} className="flex flex-wrap justify-between gap-3 border-t pt-3 text-sm">
            <div>
              <p>
                {v.display_name} · {v.email}
              </p>
              <p className="text-muted-foreground">
                {v.mailbox_address} ·{" "}
                {v.consumed_at
                ? t("已激活", "Activated")
                : v.revoked_at
                    ? t("已撤销", "Revoked")
                    : new Date(v.expires_at) < new Date()
                        ? t("已过期", "Expired")
                        : t("等待激活", "Pending activation")}
              </p>
            </div>
            {!v.consumed_at && !v.revoked_at && (<ActionButton disabled={busy} onClick={() => run(async () => {
                    await company(`/invitations/${v.id}`, { method: "DELETE" });
                    await invitations.mutate();
                })}>
                {t("撤销邀请", "Revoke invitation")}
              </ActionButton>)}
          </div>))}
      </Section>
      <Section title={t("成员与离职交接", "Members and offboarding")}>
        <p className="text-sm">
          <Link href="/company/access" className="underline">
            {t("管理账号状态、角色与权限配置", "Manage account status, roles and permission profiles")}
          </Link>
        </p>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr>
                <th>{t("成员", "Member")}</th>
                <th>{t("角色", "Role")}</th>
                <th>{t("状态", "Status")}</th>
              </tr>
            </thead>
            <tbody>
              {(members.data ?? []).map((v) => (<tr className="border-t" key={v.id}>
                  <td className="py-3">
                    {v.display_name}{" "}
                    <span className="text-muted-foreground">{v.email}</span>
                  </td>
                  <td>{v.role}</td>
                  <td>
                    {v.is_active ? t("启用", "Active") : t("停用", "Disabled")}
                  </td>
                </tr>))}
            </tbody>
          </table>
        </div>
      </Section>
    <OffboardingPanel employees={members.data ?? []} refresh={async () => { await members.mutate(); await invitations.mutate(); }}/>
    </div>;
}
