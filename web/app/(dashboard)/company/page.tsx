"use client";
import { useState } from "react";
import Link from "next/link";
import useSWR from "swr";
import { useCompany } from "@/hooks/use-company";
import { useAuth } from "@/contexts/auth-context";
import { request } from "@/lib/api/base";
import type { APIResponse } from "@/lib/types";
import {
  companyMembers,
  companyGrants,
  companyMailboxes,
  companyTemplates,
  enableCompany,
  provisionEmployee,
  reinviteEmployee,
  updateEmployee,
  createSharedMailbox,
  saveGrant,
  saveTemplate,
  setTemplateStatus,
  companyError,
  type CompanyMember,
  type MailboxGrant,
  type TemplateDraft,
} from "@/lib/api/company";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
const selectStyle = "rounded-md border bg-background p-2 text-sm";
const roles: Record<CompanyMember["company_role"], string> = {
  admin: "公司管理员",
  employee: "普通员工",
  restricted: "模板受限员工",
  viewer: "只读员工",
};
const blankDraft: TemplateDraft = {
  name: "",
  subject: "",
  text_body: "",
  html_body: "",
  variables: {},
  mailbox_ids: [],
};
export default function CompanyPage() {
  const state = useCompany();
  const company = state.data?.data.company;
  const member = state.data?.data.member;
  const { user, tenantId } = useAuth();
  const isAdmin = member?.is_active && member.company_role === "admin";
  const scope = isAdmin && company ? [company.tenant_id, user?.id] : null;
  const domains = useSWR(
    !company && user ? ["setup-domains", user.id, tenantId] : null,
    () =>
      request<
        APIResponse<
          {
            id: string;
            domain: string;
            is_verified: boolean;
            mx_verified: boolean;
          }[]
        >
      >("/api/v1/domains"),
  );
  const members = useSWR(
    scope ? ["company-members", ...scope] : null,
    companyMembers,
  );
  const [mailboxPage, setMailboxPage] = useState(1);
  const mailboxes = useSWR(
    scope ? ["company-all-mailboxes", ...scope, mailboxPage] : null,
    () => companyMailboxes(true, mailboxPage),
  );
  const grants = useSWR(scope ? ["company-all-grants", ...scope] : null, () =>
    companyGrants(true),
  );
  const templates = useSWR(
    scope ? ["company-all-templates", ...scope] : null,
    () => companyTemplates(true),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [name, setName] = useState("");
  const [zone, setZone] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [local, setLocal] = useState("");
  const [display, setDisplay] = useState("");
  const [role, setRole] = useState<CompanyMember["company_role"]>("employee");
  const [quota, setQuota] = useState(500);
  const [activation, setActivation] = useState("");
  const [shared, setShared] = useState("");
  const [grant, setGrant] = useState<MailboxGrant>({
    mailbox_id: "",
    user_id: "",
    address: "",
    can_read: true,
    can_organize: false,
    can_send: false,
    template_only: false,
  });
  const [draft, setDraft] = useState<TemplateDraft>(blankDraft);
  const [variableSpec, setVariableSpec] = useState("");
  const [section, setSection] = useState<
    "employees" | "mailboxes" | "templates"
  >("employees");
  async function action(work: () => Promise<unknown>, message: string) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await work();
      setNotice(message);
      await Promise.all([
        state.mutate(),
        members.mutate(),
        mailboxes.mutate(),
        grants.mutate(),
        templates.mutate(),
      ]);
    } catch (e) {
      setError(companyError(e));
    } finally {
      setBusy(false);
    }
  }
  function templateVariables(): Record<string, number> {
    const out: Record<string, number> = {};
    for (const line of variableSpec
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean)) {
      const parts = line.split(":");
      if (
        parts.length !== 2 ||
        !/^[a-z][a-z0-9_]*$/.test(parts[0]) ||
        !/^\d+$/.test(parts[1])
      )
        throw new Error("变量格式为 name:80，每行一个");
      if (parts[0] in out) throw new Error("变量名重复");
      out[parts[0]] = Number(parts[1]);
    }
    return out;
  }
  if (state.error)
    return (
      <div className="p-8">
        <p role="alert">{companyError(state.error)}</p>
        <Button onClick={() => state.mutate()}>重试</Button>
      </div>
    );
  if (state.isLoading) return <p className="p-8">正在读取公司配置…</p>;
  if (!company)
    return (
      <main className="mx-auto max-w-2xl space-y-6 p-8">
        <h1 className="text-2xl font-semibold">启用公司邮件治理</h1>
        <p>
          管理员先在原有域名控制台配置并验证唯一主域名，再启用公司模式。服务器需要
          full 命名，并关闭 plus-tag stripping。
        </p>
        <label className="block space-y-2">
          公司名称
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="block space-y-2">
          主域名
          <select
            className={selectStyle + " w-full"}
            value={zone}
            onChange={(e) => setZone(e.target.value)}
          >
            <option value="">选择已验证域名</option>
            {domains.data?.data.map((d) => (
              <option
                key={d.id}
                value={d.id}
                disabled={!d.is_verified || !d.mx_verified}
              >
                {d.domain}
                {d.is_verified && d.mx_verified ? "" : "（未完成验证）"}
              </option>
            ))}
          </select>
        </label>
        {domains.error && <p role="alert">{companyError(domains.error)}</p>}
        <div className="space-y-3 rounded-lg border border-amber-500/40 p-4">
          <h2 className="font-semibold">启用前请备份并确认</h2>
          <p className="text-sm">
            现有其他账号将改为只读员工并收回旧管理员权限。此操作关闭公开邮箱、自动建箱、公开注册与旧 API
            Key；阻断旧待发任务；保留历史邮件，但不会自动把旧邮箱分配给任何人。主域名在本阶段不能随意切换。
          </p>
          <label className="flex items-start gap-2">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            <span>我已备份数据，并确认进行上述权限收紧。</span>
          </label>
        </div>
        {error && (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}
        <Button
          disabled={busy || !confirmed || !zone || !name.trim()}
          onClick={() =>
            action(() => enableCompany(zone, name), "公司模式已启用")
          }
        >
          启用公司模式
        </Button>
        <p>
          <Link className="underline" href="/console/domains">
            返回域名配置
          </Link>
        </p>
      </main>
    );
  if (!isAdmin)
    return (
      <main className="p-8">
        <p>你没有公司管理权限。</p>
        <Link href="/mail" className="underline">
          返回我的邮件
        </Link>
      </main>
    );
  return (
    <main className="space-y-6 p-6 md:p-8">
      <header>
        <h1 className="text-2xl font-semibold">{company.name} · 公司治理</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          主域名：{company.domain}
          。邮件自动过期清理已对本公司关闭；邮箱管理与内容访问分开授权。
        </p>
      </header>
      <nav className="flex gap-2">
        {(
          [
            ["employees", "员工与权限层级"],
            ["mailboxes", "邮箱与共享授权"],
            ["templates", "发件模板"],
          ] as const
        ).map(([key, label]) => (
          <Button
            key={key}
            variant={section === key ? "default" : "outline"}
            onClick={() => setSection(key)}
          >
            {label}
          </Button>
        ))}
      </nav>
      {error && (
        <p
          role="alert"
          className="rounded border border-destructive p-3 text-sm text-destructive"
        >
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="rounded border p-3 text-sm">
          {notice}
        </p>
      )}
      {(members.error ||
        mailboxes.error ||
        grants.error ||
        templates.error) && (
        <p role="alert">管理数据加载失败，请刷新后重试。</p>
      )}
      {activation && (
        <div className="space-y-3 rounded-lg border border-amber-500/40 p-4">
          <strong>一次性激活码 · 72 小时有效</strong>
          <p className="text-sm">
            请通过可信渠道交给对应员工。系统未自动发送邮件，也不会在刷新后再次显示原码。员工打开{" "}
            <Link href="/activate" className="underline">
              /activate
            </Link>{" "}
            自行设置密码。
          </p>
          <Input
            aria-label="一次性激活码"
            readOnly
            type="password"
            value={activation}
          />
          <Button
            variant="outline"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(activation);
                setNotice("激活码已复制");
              } catch {
                setError("无法访问剪贴板，请选中激活码复制");
              }
            }}
          >
            复制激活码
          </Button>
          <Button variant="ghost" onClick={() => setActivation("")}>
            隐藏并清除
          </Button>
        </div>
      )}
      {section === "employees" && (
        <>
          <section className="space-y-4 rounded-lg border p-5">
            <h2 className="text-lg font-semibold">开通员工</h2>
            <div className="grid gap-4 md:grid-cols-2">
              <label className="space-y-2">
                邮箱用户名
                <Input
                  value={local}
                  onChange={(e) => setLocal(e.target.value)}
                  placeholder="alice"
                />
                <span className="text-xs text-muted-foreground">
                  @{company.domain}
                </span>
              </label>
              <label className="space-y-2">
                员工姓名
                <Input
                  value={display}
                  onChange={(e) => setDisplay(e.target.value)}
                />
              </label>
              <label className="space-y-2">
                权限层级
                <select
                  className={selectStyle + " block w-full"}
                  value={role}
                  onChange={(e) =>
                    setRole(e.target.value as CompanyMember["company_role"])
                  }
                >
                  {Object.entries(roles)
                    .filter(
                      ([k]) =>
                        k !== "admin" || member.user_id === company.owner_id,
                    )
                    .map(([key, label]) => (
                      <option key={key} value={key}>
                        {label}
                      </option>
                    ))}
                </select>
              </label>
              <label className="space-y-2">
                每日发件额度
                <Input
                  type="number"
                  min={1}
                  max={10000}
                  value={quota}
                  onChange={(e) => setQuota(Number(e.target.value))}
                />
              </label>
            </div>
            <Button
              disabled={busy || !local || !display}
              onClick={() =>
                action(async () => {
                  const r = await provisionEmployee(
                    local,
                    display,
                    role,
                    quota,
                  );
                  setActivation(r.data.activation_token);
                  setLocal("");
                  setDisplay("");
                }, "员工与个人邮箱已创建，等待员工激活")
              }
            >
              创建员工和个人邮箱
            </Button>
          </section>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b">
                  <th className="p-3">员工</th>
                  <th>层级</th>
                  <th>每日额度</th>
                  <th>账号状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {members.data?.data.map((m) => (
                  <tr key={m.user_id} className="border-b">
                    <td className="p-3">
                      <strong>{m.display_name}</strong>
                      <br />
                      {m.email}
                      {m.user_id === company.owner_id && (
                        <span className="ml-2 text-xs">公司负责人</span>
                      )}
                    </td>
                    <td>
                      <select
                        aria-label={`${m.display_name}权限层级`}
                        className={selectStyle}
                        value={m.company_role}
                        disabled={
                          busy ||
                          m.user_id === company.owner_id ||
                          m.user_id === member.user_id
                        }
                        onChange={(e) =>
                          action(
                            () =>
                              updateEmployee({
                                ...m,
                                company_role: e.target
                                  .value as CompanyMember["company_role"],
                              }),
                            "员工层级已更新",
                          )
                        }
                      >
                        {Object.entries(roles).map(([key, label]) => (
                          <option key={key} value={key}>
                            {label}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td>{m.daily_send_quota}</td>
                    <td>{m.is_active ? "已启用" : "未激活或已停用"}</td>
                    <td className="space-x-2">
                      {m.user_id !== company.owner_id &&
                        m.user_id !== member.user_id && (
                          <Button
                            variant="outline"
                            disabled={busy}
                            onClick={() => {
                              if (
                                window.confirm(
                                  m.is_active
                                    ? "停用账号并撤销凭证、阻断待发邮件？"
                                    : "重新启用已激活的员工账号？",
                                )
                              )
                                void action(
                                  () =>
                                    updateEmployee({
                                      ...m,
                                      is_active: !m.is_active,
                                    }),
                                  "账号状态已更新",
                                );
                            }}
                          >
                            {m.is_active ? "停用" : "启用"}
                          </Button>
                        )}
                      {!m.is_active && (
                        <Button
                          variant="outline"
                          disabled={busy}
                          onClick={() =>
                            action(
                              async () =>
                                setActivation(
                                  (await reinviteEmployee(m.user_id)).data
                                    .activation_token,
                                ),
                              "新激活码已生成，旧码失效",
                            )
                          }
                        >
                          重发激活码
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {section === "mailboxes" && (
        <>
          <section className="space-y-3 rounded-lg border p-5">
            <h2 className="text-lg font-semibold">创建共享邮箱</h2>
            <p className="text-sm text-muted-foreground">
              共享邮箱没有公共密码；创建后必须单独分配阅读或代发权限。
            </p>
            <div className="flex gap-3">
              <Input
                aria-label="共享邮箱用户名"
                value={shared}
                onChange={(e) => setShared(e.target.value)}
                placeholder="sales"
              />
              <Button
                disabled={busy || !shared}
                onClick={() =>
                  action(async () => {
                    await createSharedMailbox(shared);
                    setShared("");
                  }, "共享邮箱已创建，尚未分配权限")
                }
              >
                创建
              </Button>
            </div>
          </section>
          <section className="space-y-4 rounded-lg border p-5">
            <h2 className="text-lg font-semibold">明确授予或撤回邮箱权限</h2>
            <div className="grid gap-4 md:grid-cols-2">
              <label>
                员工
                <select
                  className={selectStyle + " block w-full"}
                  value={grant.user_id}
                  onChange={(e) =>
                    setGrant({ ...grant, user_id: e.target.value })
                  }
                >
                  <option value="">请选择</option>
                  {members.data?.data.map((m) => (
                    <option key={m.user_id} value={m.user_id}>
                      {m.display_name} · {m.email}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                邮箱
                <select
                  className={selectStyle + " block w-full"}
                  value={grant.mailbox_id}
                  onChange={(e) =>
                    setGrant({ ...grant, mailbox_id: e.target.value })
                  }
                >
                  <option value="">请选择</option>
                  {mailboxes.data?.data.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.full_address}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <div className="flex flex-wrap gap-5">
              {(
                [
                  ["can_read", "阅读正文"],
                  ["can_organize", "整理邮件"],
                  ["can_send", "以此地址发件"],
                  ["template_only", "仅允许模板发件"],
                ] as const
              ).map(([key, label]) => (
                <label key={key} className="flex gap-2">
                  <input
                    type="checkbox"
                    checked={grant[key]}
                    onChange={(e) =>
                      setGrant({ ...grant, [key]: e.target.checked })
                    }
                  />
                  {label}
                </label>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              保存会替换这名员工对该邮箱的完整授权；取消全部能力即可撤权。模板受限员工始终受模板规则约束。
            </p>
            <Button
              disabled={busy || !grant.user_id || !grant.mailbox_id}
              onClick={() => action(() => saveGrant(grant), "邮箱授权已更新")}
            >
              保存授权
            </Button>
          </section>
          <div className="flex items-center gap-3">
            <span>
              邮箱共 {mailboxes.data?.meta?.total ?? 0} 个 · 第 {mailboxPage} 页
            </span>
            <Button
              variant="outline"
              disabled={mailboxPage === 1}
              onClick={() => setMailboxPage(mailboxPage - 1)}
            >
              上一页
            </Button>
            <Button
              variant="outline"
              disabled={mailboxPage * 100 >= (mailboxes.data?.meta?.total ?? 0)}
              onClick={() => setMailboxPage(mailboxPage + 1)}
            >
              下一页
            </Button>
          </div>
          <div className="divide-y rounded-lg border">
            {grants.data?.data.map((g) => (
              <div
                className="flex flex-wrap items-center justify-between gap-3 p-3 text-sm"
                key={g.mailbox_id + g.user_id}
              >
                <span>
                  {g.address} →{" "}
                  {members.data?.data.find((m) => m.user_id === g.user_id)
                    ?.email ?? g.user_id}
                </span>
                <span>
                  {g.can_read ? "可读 " : ""}
                  {g.can_organize ? "可整理 " : ""}
                  {g.can_send ? "可代发 " : ""}
                  {g.template_only ? "模板限定" : ""}
                  {!g.can_read && !g.can_send ? "已撤回" : ""}
                </span>
                <Button variant="outline" size="sm" onClick={() => setGrant(g)}>
                  编辑授权
                </Button>
              </div>
            ))}
          </div>
        </>
      )}
      {section === "templates" && (
        <>
          <section className="space-y-4 rounded-lg border p-5">
            <h2 className="text-lg font-semibold">
              {draft.family_id ? "创建模板的新版本" : "设计发件模板"}
            </h2>
            <p className="text-sm text-muted-foreground">
              仅支持 {"{{.变量名}}"} 占位符。employee_name 和 company_name
              由系统填写；已保存的正文不可原地修改。保存新版本后需明确发布。
            </p>
            <label className="block space-y-2">
              模板名称
              <Input
                value={draft.name}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
            </label>
            <label className="block space-y-2">
              邮件主题
              <Input
                value={draft.subject}
                onChange={(e) =>
                  setDraft({ ...draft, subject: e.target.value })
                }
              />
            </label>
            <label className="block space-y-2">
              纯文本正文
              <Textarea
                className="min-h-40"
                value={draft.text_body}
                onChange={(e) =>
                  setDraft({ ...draft, text_body: e.target.value })
                }
              />
            </label>
            <label className="block space-y-2">
              HTML 正文（可选）
              <Textarea
                className="min-h-28 font-mono"
                value={draft.html_body}
                onChange={(e) =>
                  setDraft({ ...draft, html_body: e.target.value })
                }
              />
            </label>
            <label className="block space-y-2">
              员工必填变量及长度
              <Textarea
                value={variableSpec}
                onChange={(e) => setVariableSpec(e.target.value)}
                placeholder={"customer_name:80\nproduct:120"}
              />
            </label>
            <fieldset className="space-y-2">
              <legend className="font-medium">允许使用的发件邮箱</legend>
              {mailboxes.data?.data.map((m) => (
                <label className="mr-4 inline-flex gap-2 text-sm" key={m.id}>
                  <input
                    type="checkbox"
                    checked={draft.mailbox_ids.includes(m.id)}
                    onChange={(e) =>
                      setDraft({
                        ...draft,
                        mailbox_ids: e.target.checked
                          ? [...draft.mailbox_ids, m.id]
                          : draft.mailbox_ids.filter((id) => id !== m.id),
                      })
                    }
                  />
                  {m.full_address}
                </label>
              ))}
            </fieldset>
            <Button
              disabled={
                busy ||
                !draft.name ||
                !draft.subject ||
                !draft.mailbox_ids.length
              }
              onClick={() =>
                action(async () => {
                  await saveTemplate({
                    ...draft,
                    variables: templateVariables(),
                  });
                  setDraft(blankDraft);
                  setVariableSpec("");
                }, "模板草稿已保存；发布后授权员工才能使用")
              }
            >
              保存为新草稿版本
            </Button>
            <Button
              variant="ghost"
              onClick={() => {
                setDraft(blankDraft);
                setVariableSpec("");
              }}
            >
              清空编辑器
            </Button>
          </section>
          <div className="divide-y rounded-lg border">
            {templates.data?.data.map((t) => (
              <article className="space-y-2 p-4" key={t.id}>
                <h3 className="font-semibold">
                  {t.name} · v{t.version}{" "}
                  <span className="ml-3 text-sm font-normal">{t.status}</span>
                </h3>
                <p className="text-sm text-muted-foreground">{t.subject}</p>
                <div className="flex gap-2">
                  {t.status === "draft" && (
                    <Button
                      disabled={busy}
                      onClick={() =>
                        action(
                          () => setTemplateStatus(t.id, "published"),
                          "模板已发布",
                        )
                      }
                    >
                      发布
                    </Button>
                  )}
                  {t.status !== "retired" && (
                    <Button
                      variant="outline"
                      disabled={busy}
                      onClick={() => {
                        if (
                          window.confirm(
                            "撤回后，尚未投递的对应模板邮件也会被阻断。确认撤回？",
                          )
                        )
                          void action(
                            () => setTemplateStatus(t.id, "retired"),
                            "模板已撤回",
                          );
                      }}
                    >
                      撤回
                    </Button>
                  )}
                  <Button
                    variant="outline"
                    onClick={() => {
                      setDraft({
                        family_id: t.family_id,
                        name: t.name,
                        subject: t.subject,
                        text_body: t.text_body,
                        html_body: t.html_body,
                        variables: t.variables,
                        mailbox_ids: t.mailbox_ids,
                      });
                      setVariableSpec(
                        Object.entries(t.variables)
                          .map(([k, n]) => `${k}:${n}`)
                          .join("\n"),
                      );
                      window.scrollTo({ top: 0, behavior: "smooth" });
                    }}
                  >
                    以此创建新版本
                  </Button>
                </div>
              </article>
            ))}
          </div>
        </>
      )}
    </main>
  );
}
