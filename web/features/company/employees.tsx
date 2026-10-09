"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { ActionButton, Field, inputClass, LoadError, Section, useAction, useText } from "@/components/company/common";
import Link from "next/link";
import { company, allEmployees, type CompanySettings, type Invitation } from "@/lib/company";
import { sessionScope, useSessionScope } from "@/lib/session";
import { OffboardingPanel } from "./offboarding-panel";
export function Employees() {
    const scope = useSessionScope();
    return <EmployeesSession key={scope} scope={scope}/>;
}
function EmployeesSession({ scope }: { scope: string }) {
    const t = useText();
    const { busy, run } = useAction();
    const settings = useAPI("company-settings", () => company<CompanySettings | null>("/settings"));
    const members = useAPI("company-employees", allEmployees);
    async function readInvitations() {
        const values = await company<Invitation[]>("/invitations");
        if (!Array.isArray(values)) throw new Error(t("员工邀请列表响应无效，请重试加载。", "Invalid employee invitation list. Retry loading."));
        return values;
    }
    const invitations = useAPI("company-invitations", readInvitations);
    const [email, setEmail] = useState("");
    const [name, setName] = useState("");
    const [local, setLocal] = useState("");
    const [activation, setActivation] = useState<{ url: string; invitation: Invitation } | null>(null);
    const [readbackError, setReadbackError] = useState<Error | null>(null);
    const [checkingInvitations, setCheckingInvitations] = useState(false);
    const lifetime = useRef<object | null>(null);
    const draft = useRef<object>({});
    useLayoutEffect(() => {
        lifetime.current = {};
        return () => { lifetime.current = null; };
    }, []);
    const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();
    const settingsReady = !!settings.data && !settings.error && !settings.isLoading && !settings.isValidating;
    const membersReady = !members.error && !members.isLoading && !members.isValidating && Array.isArray(members.data);
    const invitationsReady = !invitations.error && !readbackError && !invitations.isLoading &&
        !invitations.isValidating && !checkingInvitations && Array.isArray(invitations.data);
    const validInvite = settingsReady && invitationsReady && !!email.trim() && !!local.trim() && !!name.trim();
    function edit(set: (value: string) => void, value: string) {
        // Every edit is new intent, even when A → B → A restores the same text.
        draft.current = {};
        set(value);
    }
    async function refreshInvitations(owner: object) {
        if (!owns(owner)) return;
        setCheckingInvitations(true);
        try {
            // A plain SWR revalidation may resolve to retained cached data on
            // failure. An explicit read promise confirms this readback.
            await invitations.mutate(async () => {
                const fresh = await readInvitations();
                if (!owns(owner)) throw new DOMException("Employee page changed", "AbortError");
                return fresh;
            }, { revalidate: false });
            if (owns(owner)) setReadbackError(null);
        } finally {
            if (owns(owner)) setCheckingInvitations(false);
        }
    }
    async function createInvitation() {
        const owner = lifetime.current;
        if (!owner || !owns(owner) || !validInvite) return;
        const submitted = draft.current;
        try {
            const value = await company<{ activation_token: string; invitation: Invitation }>("/invitations", {
                method: "POST", body: { email, display_name: name, local_part: local },
            });
            if (!owns(owner)) return;
            if (!value || typeof value.activation_token !== "string" || !/^[a-f0-9]{64}$/.test(value.activation_token) ||
                !value.invitation || typeof value.invitation.id !== "string" || !value.invitation.id ||
                typeof value.invitation.email !== "string" || typeof value.invitation.display_name !== "string" ||
                typeof value.invitation.mailbox_address !== "string") {
                throw new Error(t("无法确认邀请响应，请重新加载列表核对。", "Could not confirm the invitation response. Reload the list to review."));
            }
            // The secret can only be read once. Preserve an acknowledged result
            // with its server-returned recipient while keeping newer form input.
            setActivation({ url: `${window.location.origin}/auth/activate#${value.activation_token}`, invitation: value.invitation });
            if (draft.current === submitted) {
                draft.current = {};
                setEmail(""); setName(""); setLocal("");
            }
            toast.success(t("邀请已生成", "Invitation created"));
            try { await refreshInvitations(owner); } catch {
                if (owns(owner)) setReadbackError(new Error(t("邀请已生成，但邀请列表刷新失败。请保留激活链接并重试加载。", "The invitation was created, but the invitation list could not be refreshed. Keep this activation link and retry loading.")));
            }
        } catch (error) {
            if (owns(owner)) throw error;
        }
    }
    async function revokeInvitation(value: Invitation) {
        const owner = lifetime.current;
        if (!owner || !owns(owner) || !invitationsReady || !invitations.data?.includes(value) || value.consumed_at || value.revoked_at) return;
        try {
            await company(`/invitations/${encodeURIComponent(value.id)}`, { method: "DELETE" });
            if (!owns(owner)) return;
            setActivation(current => current?.invitation.id === value.id ? null : current);
            try { await refreshInvitations(owner); } catch {
                if (owns(owner)) setReadbackError(new Error(t("邀请已撤销，但列表刷新失败。请重试加载以核对当前状态。", "The invitation was revoked, but the list could not be refreshed. Retry loading to check its current state.")));
            }
        } catch (error) {
            if (owns(owner)) throw error;
        }
    }
    return <div className="space-y-5"><LoadError error={readbackError || members.error || invitations.error || settings.error} onRetry={() => {
        const owner = lifetime.current;
        if (!owner || !owns(owner)) return;
        void run(async () => {
            try { await Promise.all([members.mutate(), settings.mutate(), refreshInvitations(owner)]); }
            catch (error) { if (owns(owner)) throw error; }
        });
    }}/>
      <Section title={t("邀请员工并分配个人邮箱", "Invite an employee and assign a personal mailbox")}>
        <p className="text-sm text-muted-foreground">
          {t("激活链接有效 72 小时，只可使用一次。请通过可信渠道交付；此操作不会自动发送外部邮件。", "Activation links expire after 72 hours and are single-use. Deliver through a trusted channel; this action does not send external mail.")}
        </p>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("登录邮箱", "Login email")}>
            {(id) => (<input id={id} className={inputClass} type="email" value={email} onChange={(e) => edit(setEmail, e.target.value)}/>)}
          </Field>
          <Field label={t("姓名", "Display name")}>
            {(id) => (<input id={id} className={inputClass} value={name} onChange={(e) => edit(setName, e.target.value)}/>)}
          </Field>
          <Field label={t("公司邮箱用户名", "Company mailbox local part")}>
            {(id) => (<input id={id} className={inputClass} value={local} placeholder={`alice @ ${settings.data?.domain ?? "company"}`} onChange={(e) => edit(setLocal, e.target.value)}/>)}
          </Field>
        </div>
        <ActionButton disabled={busy || !validInvite} onClick={() => run(createInvitation)}>
          {t("生成员工邀请", "Create employee invitation")}
        </ActionButton>
        {activation && (<div className="rounded border p-3 space-y-2">
            <p className="break-all text-sm">{t("邀请对象", "Invitation for")}: {activation.invitation.display_name} · {activation.invitation.email} · {activation.invitation.mailbox_address}</p>
            <Field label={t("一次性激活链接（离开页面后不再显示）", "One-time activation link (not retained after leaving)")}>
              {(id) => (<input id={id} className={inputClass} readOnly value={activation.url}/>)}
            </Field>
            <ActionButton disabled={busy} onClick={() => run(async () => {
                const owner = lifetime.current;
                if (!owner || !owns(owner)) return;
                try {
                    await navigator.clipboard.writeText(activation.url);
                    if (owns(owner)) toast.success(t("已复制", "Copied"));
                } catch (error) { if (owns(owner)) throw error; }
            })}>
              {t("复制激活链接", "Copy activation link")}
            </ActionButton>
          </div>)}
        {!invitations.error && !readbackError && (invitations.data ?? []).map((v) => (<div key={v.id} className="flex flex-wrap justify-between gap-3 border-t pt-3 text-sm">
            <div>
              <p>
                {v.display_name} · {v.email}
              </p>
              <p className="text-muted-foreground">
                {v.mailbox_address} ·{" "}
                <span>{v.consumed_at
                ? t("已激活", "Activated")
                : v.revoked_at
                    ? t("已撤销", "Revoked")
                    : new Date(v.expires_at) < new Date()
                        ? t("已过期", "Expired")
                        : t("等待激活", "Pending activation")}</span>
              </p>
            </div>
            {!v.consumed_at && !v.revoked_at && (<ActionButton disabled={busy || !invitationsReady} onClick={() => run(() => revokeInvitation(v))}>
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
        {(members.isLoading || members.isValidating) && <p role="status" className="text-sm text-muted-foreground">
          {members.data ? t("正在刷新成员…", "Refreshing members…") : t("正在加载成员…", "Loading members…")}
        </p>}
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
              {(membersReady ? members.data ?? [] : []).map((v) => (<tr className="border-t" key={v.id}>
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
        {membersReady && members.data?.length === 0 && <p className="text-sm text-muted-foreground">{t("暂无成员", "No members")}</p>}
      </Section>
    <OffboardingPanel employees={members.data ?? []} employeesReady={membersReady} refresh={async () => { await members.mutate(); await invitations.mutate(); }}/>
    </div>;
}
