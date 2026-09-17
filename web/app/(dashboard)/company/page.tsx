"use client";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { useAuth } from "@/contexts/auth-context";
import { useAPI } from "@/hooks/use-api";
import { request } from "@/lib/api/base";
import type { APIListResponse, DomainZone, AdminUser } from "@/lib/types";
import {
  allEmployees,
  company,
  workMailboxes,
  workPath,
  type CompanySettings,
  type Invitation,
  type WorkMailbox,
  type WorkGrant,
} from "@/lib/company";
import { EmployeeField } from "@/components/company/employee-field";
import { CompanyDomainsSection } from "@/components/company/domains";
import {
  CompanyMailSendPolicyField,
  MailboxSendPolicyEditor,
  sendPolicyDescription,
} from "@/components/company/send-policy";
import {
  ActionButton,
  Field,
  inputClass,
  LoadError,
  Section,
  useAction,
  useText,
} from "@/components/company/common";

export default function CompanyPage() {
  const t = useText();
  const { user } = useAuth();
  const { busy, run } = useAction();
  const settings = useAPI("company-settings", () =>
    company<CompanySettings | null>("/settings"),
  );
  const [editing, setEditing] = useState<CompanySettings | null>(null);
  const config = editing ??
    settings.data ?? { name: "", primary_zone_id: "", revision: 0 };
  const zones = useAPI("company-zones", () =>
    request<APIListResponse<DomainZone>>("/api/v1/domains", {
      params: { page: 1, per_page: 100 },
    }),
  );
  const members = useAPI("company-employees", allEmployees);
  const boxes = useAPI("company-managed-mailboxes", workMailboxes);
  const invitations = useAPI("company-invitations", () =>
    company<Invitation[]>("/invitations"),
  );
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [local, setLocal] = useState("");
  const [activation, setActivation] = useState("");
  const [target, setTarget] = useState("");
  const [successor, setSuccessor] = useState("");
  const [reason, setReason] = useState("");
  const [boxLocal, setBoxLocal] = useState("");
  const [kind, setKind] = useState("shared");
  const [owner, setOwner] = useState("");
  const [retention, setRetention] = useState(0);
  const [selected, setSelected] = useState("");
  const active = (members.data ?? []).filter((v) => v.is_active);
  const mailbox = boxes.data?.find((v) => v.mailbox.id === selected);
  async function refresh() {
    await Promise.all([members.mutate(), boxes.mutate(), invitations.mutate()]);
  }
  return (
    <main className="mx-auto w-full max-w-6xl space-y-6 p-4 md:p-7">
      <header>
        <h1 className="text-2xl font-semibold">
          {t("公司管理", "Company administration")}
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {t(
            "管理员负责开通与授权，不自动获得员工邮件正文的阅读权。",
            "Administrators provision and grant access; their role does not automatically grant message-content access.",
          )}
        </p>
      </header>
      <LoadError
        error={
          settings.error ||
          members.error ||
          boxes.error ||
          invitations.error ||
          zones.error
        }
        onRetry={() => {
          void settings.mutate();
          void zones.mutate();
          void refresh();
        }}
      />
      <CompanyDomainsSection />
      <Section title={t("公司主域名", "Company primary domain")}>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("公司名称", "Company name")}>
            {(id) => (
              <input
                id={id}
                className={inputClass}
                maxLength={120}
                value={config.name}
                onChange={(e) =>
                  setEditing({ ...config, name: e.target.value })
                }
              />
            )}
          </Field>
          <Field label={t("已验证的主域名", "Verified primary domain")}>
            {(id) => (
              <select
                id={id}
                className={inputClass}
                value={config.primary_zone_id}
                onChange={(e) =>
                  setEditing({ ...config, primary_zone_id: e.target.value })
                }
              >
                <option value="">{t("请选择", "Select")}</option>
                {(zones.data?.data ?? [])
                  .filter((v) => v.is_verified && v.mx_verified)
                  .map((v) => (
                    <option key={v.id} value={v.id}>
                      {v.domain}
                    </option>
                  ))}
              </select>
            )}
          </Field>
          <CompanyMailSendPolicyField
            value={config.mail_send_policy ?? "free"}
            onChange={(policy) =>
              setEditing({ ...config, mail_send_policy: policy })
            }
          />
        </div>
        <p className="text-sm text-muted-foreground">
          {t(
            "发送策略约束该公司所有邮箱的对外发送（管理员与属主同样受限）：",
            "The send policy governs outbound sending for every company mailbox (admins and owners included): ",
          )}
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
        <ActionButton
          disabled={busy || !config.name.trim() || !config.primary_zone_id}
          onClick={() =>
            run(async () => {
              await company("/settings", { method: "PUT", body: config });
              await settings.mutate();
              setEditing(null);
              toast.success(t("公司设置已保存", "Company settings saved"));
            })
          }
        >
          {t("保存公司设置", "Save company settings")}
        </ActionButton>
      </Section>
      <Section
        title={t(
          "邀请员工并分配个人邮箱",
          "Invite an employee and assign a personal mailbox",
        )}
      >
        <p className="text-sm text-muted-foreground">
          {t(
            "激活链接有效 72 小时，只可使用一次。请通过可信渠道交付；此操作不会自动发送外部邮件。",
            "Activation links expire after 72 hours and are single-use. Deliver through a trusted channel; this action does not send external mail.",
          )}
        </p>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("登录邮箱", "Login email")}>
            {(id) => (
              <input
                id={id}
                className={inputClass}
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            )}
          </Field>
          <Field label={t("姓名", "Display name")}>
            {(id) => (
              <input
                id={id}
                className={inputClass}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            )}
          </Field>
          <Field label={t("公司邮箱用户名", "Company mailbox local part")}>
            {(id) => (
              <input
                id={id}
                className={inputClass}
                value={local}
                placeholder={`alice @ ${settings.data?.domain ?? "company"}`}
                onChange={(e) => setLocal(e.target.value)}
              />
            )}
          </Field>
        </div>
        <ActionButton
          disabled={busy || !email || !local || !name || !settings.data}
          onClick={() =>
            run(async () => {
              const v = await company<{ activation_token: string }>(
                "/invitations",
                {
                  method: "POST",
                  body: { email, display_name: name, local_part: local },
                },
              );
              setActivation(
                `${window.location.origin}/auth/activate#${v.activation_token}`,
              );
              setEmail("");
              setName("");
              setLocal("");
              await invitations.mutate();
              toast.success(t("邀请已生成", "Invitation created"));
            })
          }
        >
          {t("生成员工邀请", "Create employee invitation")}
        </ActionButton>
        {activation && (
          <div className="rounded border p-3 space-y-2">
            <Field
              label={t(
                "一次性激活链接（离开页面后不再显示）",
                "One-time activation link (not retained after leaving)",
              )}
            >
              {(id) => (
                <input
                  id={id}
                  className={inputClass}
                  readOnly
                  value={activation}
                />
              )}
            </Field>
            <ActionButton
              onClick={() =>
                run(async () => {
                  await navigator.clipboard.writeText(activation);
                  toast.success(t("已复制", "Copied"));
                })
              }
            >
              {t("复制激活链接", "Copy activation link")}
            </ActionButton>
          </div>
        )}
        {(invitations.data ?? []).map((v) => (
          <div
            key={v.id}
            className="flex flex-wrap justify-between gap-3 border-t pt-3 text-sm"
          >
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
            {!v.consumed_at && !v.revoked_at && (
              <ActionButton
                disabled={busy}
                onClick={() =>
                  run(async () => {
                    await company(`/invitations/${v.id}`, { method: "DELETE" });
                    await invitations.mutate();
                  })
                }
              >
                {t("撤销邀请", "Revoke invitation")}
              </ActionButton>
            )}
          </div>
        ))}
      </Section>
      <Section title={t("成员与离职交接", "Members and offboarding")}>
        <p className="text-sm">
          <Link href="/admin/users" className="underline">
            {t(
              "管理账号状态、角色与权限配置",
              "Manage account status, roles and permission profiles",
            )}
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
              {(members.data ?? []).map((v) => (
                <tr className="border-t" key={v.id}>
                  <td className="py-3">
                    {v.display_name}{" "}
                    <span className="text-muted-foreground">{v.email}</span>
                  </td>
                  <td>{v.role}</td>
                  <td>
                    {v.is_active ? t("启用", "Active") : t("停用", "Disabled")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="text-sm text-muted-foreground">
          {t(
            "离职操作在同一事务中停用账号、撤销会话与个人密钥、移除共享授权并交接个人邮箱。不会删除邮件。",
            "Offboarding atomically disables the account, revokes sessions and personal keys, removes shared grants and transfers personal mailboxes. Messages are retained.",
          )}
        </p>
        <div className="grid gap-4 md:grid-cols-2">
          <EmployeeField
            label={t("停用成员", "Employee to offboard")}
            value={target}
            onChange={setTarget}
            employees={active.filter(
              (v) =>
                v.id !== user?.id &&
                (user?.role === "super_admin" || v.role === "user"),
            )}
          />
          <EmployeeField
            label={t("邮箱接管人", "Mailbox successor")}
            value={successor}
            onChange={setSuccessor}
            employees={active.filter((v) => v.id !== target)}
          />
        </div>
        <Field
          label={t(
            "交接原因 / 工单号（至少 8 个字符）",
            "Reason / ticket (at least 8 characters)",
          )}
        >
          {(id) => (
            <input
              id={id}
              className={inputClass}
              value={reason}
              minLength={8}
              maxLength={1000}
              onChange={(e) => setReason(e.target.value)}
            />
          )}
        </Field>
        <ActionButton
          disabled={busy || !target || !successor || reason.trim().length < 8}
          onClick={() =>
            run(async () => {
              if (
                !window.confirm(
                  t(
                    "确认停用该成员并移交其个人邮箱？",
                    "Disable this member and transfer their personal mailboxes?",
                  ),
                )
              )
                return;
              await company(`/employees/${target}/offboard`, {
                method: "POST",
                body: { successor_user_id: successor, reason },
              });
              setTarget("");
              setReason("");
              await refresh();
              toast.success(
                t(
                  "账号已停用，邮箱已交接",
                  "Account disabled and mailboxes transferred",
                ),
              );
            })
          }
        >
          {t("执行离职交接", "Offboard and transfer")}
        </ActionButton>
      </Section>
      <Section title={t("创建长期公司邮箱", "Create a company mailbox")}>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("邮箱用户名", "Mailbox local part")}>
            {(id) => (
              <input
                id={id}
                className={inputClass}
                value={boxLocal}
                onChange={(e) => setBoxLocal(e.target.value)}
              />
            )}
          </Field>
          <Field label={t("资源类型", "Resource type")}>
            {(id) => (
              <select
                id={id}
                className={inputClass}
                value={kind}
                onChange={(e) => setKind(e.target.value)}
              >
                <option value="shared">
                  {t("公司共享邮箱", "Company shared mailbox")}
                </option>
                <option value="personal">
                  {t("员工个人邮箱", "Personal mailbox")}
                </option>
              </select>
            )}
          </Field>
          {kind === "personal" ? (
            <EmployeeField
              label={t("邮箱属主", "Mailbox owner")}
              value={owner}
              onChange={setOwner}
              employees={active}
            />
          ) : (
            <Field
              label={t(
                "保留小时数（0 为永久）",
                "Retention hours (0 = permanent)",
              )}
            >
              {(id) => (
                <input
                  id={id}
                  className={inputClass}
                  type="number"
                  min={0}
                  max={876000}
                  step={1}
                  value={retention}
                  onChange={(e) => setRetention(Number(e.target.value))}
                />
              )}
            </Field>
          )}
        </div>
        <ActionButton
          disabled={
            busy ||
            !boxLocal ||
            (kind === "personal" && !owner) ||
            !Number.isInteger(retention) ||
            retention < 0
          }
          onClick={() =>
            run(async () => {
              await company("/mailboxes", {
                method: "POST",
                body: {
                  local_part: boxLocal,
                  kind,
                  owner_user_id: kind === "personal" ? owner : undefined,
                  retention_hours: kind === "personal" ? 0 : retention,
                },
              });
              setBoxLocal("");
              await boxes.mutate();
              toast.success(t("私有邮箱已创建", "Private mailbox created"));
            })
          }
        >
          {t("创建邮箱", "Create mailbox")}
        </ActionButton>
      </Section>
      <Section title={t("邮箱授权与交接", "Mailbox grants and handover")}>
        <Field label={t("管理邮箱", "Manage mailbox")}>
          {(id) => (
            <select
              id={id}
              className={inputClass}
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
            >
              <option value="">{t("请选择邮箱", "Select a mailbox")}</option>
              {(boxes.data ?? []).map((v) => (
                <option key={v.mailbox.id} value={v.mailbox.id}>
                  {v.mailbox.full_address} · {v.mailbox.kind ?? "legacy"}
                </option>
              ))}
            </select>
          )}
        </Field>
        {mailbox && (
          <GrantEditor
            key={`${mailbox.mailbox.id}:${mailbox.revision}`}
            mailbox={mailbox}
            employees={active}
            refresh={() => boxes.mutate()}
          />
        )}
        {mailbox && (
          <MailboxSendPolicyEditor
            key={`send-policy:${mailbox.mailbox.id}:${mailbox.revision}`}
            mailbox={mailbox}
            refresh={() => boxes.mutate()}
          />
        )}
      </Section>
    </main>
  );
}

function GrantEditor({
  mailbox,
  employees,
  refresh,
}: {
  mailbox: WorkMailbox;
  employees: AdminUser[];
  refresh: () => Promise<unknown>;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const grants = useAPI(["mailbox-grants", mailbox.mailbox.id], () =>
    company<WorkGrant[]>(`${workPath(mailbox.mailbox.id)}/grants`),
  );
  const empty: WorkGrant = {
    user_id: "",
    can_read: false,
    can_organize: false,
    can_send: false,
    template_only: false,
  };
  const [grant, setGrant] = useState(empty);
  const [nextOwner, setNextOwner] = useState("");
  const [reason, setReason] = useState("");
  return (
    <div className="space-y-4">
      <LoadError error={grants.error} onRetry={() => void grants.mutate()} />
      <p className="text-sm text-muted-foreground">
        {t(
          "属主具有内建权限；其他成员必须显式授权。全不选即撤销授权。",
          "Owners have intrinsic rights. Other members need explicit grants; clearing all rights revokes access.",
        )}
      </p>
      <EmployeeField
        label={t("授权成员", "Grant to member")}
        value={grant.user_id}
        employees={employees.filter(
          (v) => v.id !== mailbox.mailbox.owner_user_id,
        )}
        onChange={(id) =>
          setGrant(
            grants.data?.find((v) => v.user_id === id) ?? {
              ...empty,
              user_id: id,
            },
          )
        }
      />
      <div className="flex flex-wrap gap-5">
        {(
          ["can_read", "can_organize", "can_send", "template_only"] as const
        ).map((key, i) => (
          <label key={key} className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={grant[key]}
              onChange={(e) => {
                const next = { ...grant, [key]: e.target.checked };
                if (key === "can_organize" && next.can_organize)
                  next.can_read = true;
                if (key === "can_read" && !next.can_read)
                  next.can_organize = false;
                if (key === "template_only" && next.template_only)
                  next.can_send = true;
                if (key === "can_send" && !next.can_send)
                  next.template_only = false;
                setGrant(next);
              }}
            />
            {
              [
                t("阅读", "Read"),
                t("整理 / 删除", "Organize / delete"),
                t("代发", "Send as"),
                t("仅使用模板发送", "Template-only sending"),
              ][i]
            }
          </label>
        ))}
      </div>
      <ActionButton
        disabled={busy || !grant.user_id || Boolean(grants.error)}
        onClick={() =>
          run(async () => {
            await company(`${workPath(mailbox.mailbox.id)}/grants`, {
              method: "PUT",
              body: grant,
            });
            await grants.mutate();
            await refresh();
            toast.success(t("授权已更新", "Grant updated"));
          })
        }
      >
        {t("保存邮箱授权", "Save mailbox grant")}
      </ActionButton>
      {(grants.data ?? []).map((v) => (
        <p key={v.user_id} className="text-sm">
          {employees.find((u) => u.id === v.user_id)?.email ?? v.user_id}:{" "}
          {v.can_read ? t("阅读 ", "read ") : ""}
          {v.can_organize ? t("整理 ", "organize ") : ""}
          {v.can_send ? t("代发 ", "send ") : ""}
          {v.template_only ? t("仅模板", "template only") : ""}
        </p>
      ))}
      {mailbox.mailbox.kind === "personal" && (
        <EmployeeField
          label={t("新属主", "New owner")}
          value={nextOwner}
          employees={employees.filter(
            (v) => v.id !== mailbox.mailbox.owner_user_id,
          )}
          onChange={setNextOwner}
        />
      )}
      {(mailbox.mailbox.kind === "personal" ||
        mailbox.mailbox.kind === "legacy") && (
        <>
          <Field
            label={t(
              "资源变更原因（至少 8 个字符）",
              "Resource-change reason (8+ characters)",
            )}
          >
            {(id) => (
              <input
                id={id}
                className={inputClass}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            )}
          </Field>
          <ActionButton
            disabled={
              busy ||
              reason.trim().length < 8 ||
              (mailbox.mailbox.kind === "personal" && !nextOwner)
            }
            onClick={() =>
              run(async () => {
                if (mailbox.mailbox.kind === "personal")
                  await company(`${workPath(mailbox.mailbox.id)}/handover`, {
                    method: "POST",
                    body: {
                      owner_user_id: nextOwner,
                      revision: mailbox.revision,
                      reason,
                    },
                  });
                else
                  await company(
                    `${workPath(mailbox.mailbox.id)}/convert-shared`,
                    {
                      method: "POST",
                      body: { revision: mailbox.revision, reason },
                    },
                  );
                await refresh();
                toast.success(
                  t("邮箱生命周期已更新", "Mailbox lifecycle updated"),
                );
              })
            }
          >
            {mailbox.mailbox.kind === "personal"
              ? t(
                  "移交邮箱（不删除邮件）",
                  "Transfer mailbox (preserve messages)",
                )
              : t(
                  "迁移为私有共享邮箱并永久保留",
                  "Convert to private, permanently retained shared mailbox",
                )}
          </ActionButton>
        </>
      )}
    </div>
  );
}
