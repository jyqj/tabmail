"use client";
import { useState } from "react";
import { useAuth } from "@/contexts/auth-context";
import type { AdminUser } from "@/lib/types";
import { EmployeeField } from "@/components/company/employee-field";
import { ActionButton, Field, inputClass, Section, useAction, useText } from "@/components/company/common";
import { previewOffboarding, executeOffboarding, type OffboardingOptions, type OffboardingPlan } from "./api";
export function OffboardingPanel({ employees, refresh }: {
    employees: AdminUser[];
    refresh: () => Promise<unknown>;
}) {
    const t = useText();
    const { user } = useAuth();
    const { busy, run } = useAction();
    const [target, setTarget] = useState("");
    const [successor, setSuccessor] = useState("");
    const [reason, setReason] = useState("");
    const [disposition, setDisposition] = useState<OffboardingOptions["drafts"]>("seal");
    const [plan, setPlan] = useState<OffboardingPlan | null>(null);
    const [conflict, setConflict] = useState(false);
    const active = employees.filter(u => u.is_active);
    return <Section title={t("离职预览与交接", "Offboarding preview and handover")}>
  <p className="text-sm text-muted-foreground">{t("先核对资产与处置，再停用账号。预览不读取私人草稿正文，默认封存而不是转交。", "Review assets and disposition before disabling the account. Preview never reads private draft bodies; the default is sealing, not transfer.")}</p>
  {!plan ? <>
   <div className="grid gap-3 md:grid-cols-2"><EmployeeField label={t("停用成员", "Employee to offboard")} value={target} onChange={setTarget} employees={active.filter(u => u.id !== user?.id && (user?.role === "super_admin" || u.role === "user"))}/><EmployeeField label={t("邮箱接管人", "Mailbox successor")} value={successor} onChange={setSuccessor} employees={active.filter(u => u.id !== target)}/></div>
   <Field label={t("草稿处置", "Draft disposition")}>{id => <select id={id} className={inputClass} value={disposition} onChange={e => setDisposition(e.target.value as OffboardingOptions["drafts"])}><option value="seal">{t("封存全部私人草稿（默认）", "Seal all private drafts (default)")}</option><option value="transfer_owned">{t("交接本人名下邮箱草稿，其余封存", "Transfer drafts in owned mailboxes; seal the rest")}</option><option value="discard">{t("明确删除未发送草稿", "Explicitly discard unsent drafts")}</option></select>}</Field>
   <Field label={t("交接原因 / 工单号（至少 8 个字符）", "Reason / ticket (at least 8 characters)")}>{id => <input id={id} className={inputClass} value={reason} minLength={8} maxLength={1000} onChange={e => setReason(e.target.value)}/>}</Field>
   {conflict && <p role="alert" className="text-sm text-destructive">{t("资产或权限已变化，请重新预览；没有执行旧计划。", "Assets or authority changed. Preview again; the stale plan was not applied.")}</p>}
   <ActionButton disabled={busy || !target || !successor || target === successor || reason.trim().length < 8} onClick={() => run(async () => { const value = await previewOffboarding(target, successor, { drafts: disposition }, reason); setPlan(value); setConflict(false); })}>{t("预览交接影响", "Preview handover impact")}</ActionButton>
  </> : <>
   <p className="text-sm break-all">{t("计划", "Plan")}: {plan.id} · {plan.reason}</p>
   <dl className="grid grid-cols-2 gap-3 md:grid-cols-3">{(Object.keys(plan.impact) as (keyof typeof plan.impact)[]).map(k => <div key={k} className="rounded border p-3"><dt className="text-xs text-muted-foreground">{({ mailboxes: t("移交邮箱", "Mailboxes"), drafts: t("私人草稿", "Private drafts"), transferable_drafts: t("本人邮箱可移交草稿", "Drafts eligible for transfer"), attachments: t("上传附件", "Uploaded attachments"), api_keys: t("撤销个人密钥", "Keys revoked"), grants: t("移除共享授权", "Shared grants removed"), queued: t("取消未开始投递", "Queued deliveries cancelled"), in_flight: t("正在投递", "In-flight deliveries"), uncertain: t("结果不确定", "Uncertain results") })[k]}</dt><dd className="text-xl font-semibold">{plan.impact[k]}</dd></div>)}</dl>
   <p className="text-sm">{t("草稿处置", "Draft disposition")}: {({ seal: t("封存，不向接管人开放正文", "Seal; do not disclose to successor"), transfer_owned: t("只交接本人名下邮箱草稿及附件；其他草稿封存", "Transfer owned-mailbox drafts and attachments only; seal others"), discard: t("删除未发送草稿，已发送邮件不受影响", "Discard unsent drafts; sent mail is retained") })[plan.options.drafts]}</p>
   {(plan.impact.in_flight > 0 || plan.impact.uncertain > 0) && <p role="status" className="rounded border border-amber-500 p-3 text-sm">{t("正在发送和结果不确定的记录不会被伪装为取消或自动重发，需平台运维核查。已交付给 SMTP 的字节无法撤回。", "Active and uncertain deliveries are preserved for operator review, never relabeled cancelled or blindly resent. Bytes already given to SMTP cannot be recalled.")}</p>}
   {plan.state === "executed" ? <p role="status">{t("交接已完成，执行回执已保存。", "Handover completed; the disposition receipt is retained.")} {plan.executed_at}</p> : <div className="flex gap-2">
    <ActionButton disabled={busy} onClick={() => setPlan(null)}>{t("修改处置并重新预览", "Revise and preview again")}</ActionButton>
    <ActionButton disabled={busy} onClick={() => run(async () => {
                    if (!window.confirm(plan.options.drafts === "discard" ? t("确认永久删除该成员未发送草稿并执行交接？", "Permanently discard unsent drafts and execute this handover?") : t("确认按以上计划停用账号、撤销会话并移交邮箱？", "Disable the account, revoke sessions and transfer mailboxes according to this plan?")))
                        return;
                    try {
                        setPlan(await executeOffboarding(plan.target_id, plan.id));
                        await refresh();
                    }
                    catch (error) {
                        if ((error as {
                            error?: {
                                code?: string;
                            };
                        })?.error?.code === "CONFLICT") {
                            setPlan(null);
                            setConflict(true);
                        }
                        throw error;
                    }
                })}>{t("确认执行交接", "Confirm handover")}</ActionButton>
   </div>}
   <p className="text-xs text-muted-foreground">{t("预览有效期至", "Preview expires at")}: {new Date(plan.expires_at).toLocaleString()}</p>
  </>}
 </Section>;
}
