"use client";
import { useState } from "react";
import Link from "next/link";
import useSWR from "swr";
import { toast } from "sonner";
import { useCompany } from "@/hooks/use-company";
import { useAuth } from "@/contexts/auth-context";
import {
  companyGrants,
  companyTemplates,
  companyError,
  previewTemplate,
  sendCompanyMail,
} from "@/lib/api/company";
import { listMessages, getMessage } from "@/lib/api/messages";
import { listOutboundJobs } from "@/lib/api/outbound";
import { MessageDetail } from "@/components/inbox/message-detail";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

const selectStyle = "w-full rounded-md border bg-background p-2 text-sm";
export default function MailPage() {
  const state = useCompany();
  const company = state.data?.data.company;
  const member = state.data?.data.member;
  const { user } = useAuth();
  const scope =
    company && member?.is_active ? [company.tenant_id, user?.id] : null;
  const grants = useSWR(
    scope ? ["mail-grants", ...scope] : null,
    () => companyGrants(),
    { refreshInterval: 15000 },
  );
  const templates = useSWR(
    scope ? ["mail-templates", ...scope] : null,
    () => companyTemplates(),
    { refreshInterval: 15000 },
  );
  const [address, setAddress] = useState("");
  const [messageId, setMessageId] = useState("");
  const [page, setPage] = useState(1);
  const [tab, setTab] = useState<"inbox" | "compose" | "sent">("inbox");
  const readable = (grants.data?.data ?? []).filter((g) => g.can_read);
  const senders = (grants.data?.data ?? []).filter(
    (g) => g.can_send && member?.company_role !== "viewer",
  );
  const inbox = readable.some((g) => g.address === address)
    ? address
    : (readable[0]?.address ?? "");
  const messages = useSWR(
    scope && inbox ? ["mail-inbox", ...scope, inbox, page] : null,
    () => listMessages(inbox, page, 30),
    { refreshInterval: 15000 },
  );
  const detail = useSWR(
    scope && messageId && inbox
      ? ["mail-detail", ...scope, inbox, messageId]
      : null,
    () => getMessage(inbox, messageId),
  );
  const sent = useSWR(
    scope ? ["mail-sent", ...scope] : null,
    () => listOutboundJobs({ page: 1, per_page: 30 }),
    { refreshInterval: 15000 },
  );
  const [from, setFrom] = useState("");
  const sender = senders.find((g) => g.address === from) ?? senders[0];
  const [recipients, setRecipients] = useState("");
  const [templateId, setTemplateId] = useState("");
  const [variables, setVariables] = useState<Record<string, string>>({});
  const [subject, setSubject] = useState("");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<{
    subject: string;
    text_body: string;
    html_body: string;
  } | null>(null);
  const available = (templates.data?.data ?? []).filter(
    (t) =>
      t.status === "published" &&
      sender &&
      t.mailbox_ids.includes(sender.mailbox_id),
  );
  const selected = available.find((t) => t.id === templateId);
  const required =
    member?.company_role === "restricted" || Boolean(sender?.template_only);
  const resetPreview = () => setPreview(null);
  async function send() {
    setBusy(true);
    try {
      await sendCompanyMail({
        from: sender?.address ?? "",
        recipients,
        templateId: selected?.id ?? "",
        variables,
        subject,
        text,
        templateRequired: required,
      });
      toast.success("邮件已进入发送队列；投递结果请查看已发送");
      setRecipients("");
      setText("");
      setSubject("");
      setPreview(null);
      await sent.mutate();
      setTab("sent");
    } catch (e) {
      toast.error(companyError(e));
    } finally {
      setBusy(false);
    }
  }
  async function renderPreview() {
    if (!sender || !selected) return;
    setBusy(true);
    try {
      setPreview(
        (await previewTemplate(selected.id, sender.mailbox_id, variables)).data,
      );
    } catch (e) {
      toast.error(companyError(e));
    } finally {
      setBusy(false);
    }
  }
  if (state.error)
    return (
      <div className="p-8">
        <p role="alert">{companyError(state.error)}</p>
        <Button onClick={() => state.mutate()}>重试</Button>
      </div>
    );
  if (state.isLoading) return <p className="p-8">正在读取公司邮箱权限…</p>;
  if (!company)
    return (
      <div className="space-y-4 p-8">
        <h1 className="text-2xl font-semibold">公司邮件工作台</h1>
        <p>尚未启用公司模式，请由管理员配置主域名。</p>
        <Link href="/company" className="underline">
          进入公司治理
        </Link>
        <p>
          <Link href="/console/domains" className="underline">
            返回原有控制台
          </Link>
        </p>
      </div>
    );
  if (!member?.is_active)
    return (
      <p className="p-8" role="alert">
        当前账号没有有效的公司成员身份。
      </p>
    );
  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-b p-6">
        <h1 className="text-2xl font-semibold">{company.name} · 邮件</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {member.display_name} · {member.email}
        </p>
        <nav className="mt-4 flex gap-2">
          {(
            [
              ["inbox", "收件箱"],
              ["compose", "写邮件"],
              ["sent", "我的发送记录"],
            ] as const
          ).map(([key, label]) => (
            <Button
              key={key}
              variant={tab === key ? "default" : "outline"}
              onClick={() => setTab(key)}
            >
              {label}
            </Button>
          ))}
        </nav>
      </header>
      {(grants.error || templates.error) && (
        <p role="alert" className="p-4 text-destructive">
          权限加载失败，请刷新；不会使用旧权限提交邮件。
        </p>
      )}
      {tab === "inbox" && (
        <div className="grid flex-1 lg:grid-cols-[340px_1fr]">
          <section className="space-y-3 border-r p-4">
            <label htmlFor="inbox-address" className="text-sm font-medium">
              我的个人与共享邮箱
            </label>
            <select
              id="inbox-address"
              className={selectStyle}
              value={inbox}
              onChange={(e) => {
                setAddress(e.target.value);
                setPage(1);
                setMessageId("");
              }}
            >
              {readable.map((g) => (
                <option key={g.mailbox_id} value={g.address}>
                  {g.address}
                </option>
              ))}
            </select>
            {!readable.length && (
              <p className="text-sm text-muted-foreground">
                没有可读取的邮箱。请联系管理员分配。
              </p>
            )}
            {messages.error && (
              <p role="alert">{companyError(messages.error)}</p>
            )}
            <Button variant="outline" onClick={() => messages.mutate()}>
              刷新收件箱
            </Button>
            <div className="divide-y">
              {messages.data?.data.map((m) => (
                <button
                  key={m.id}
                  onClick={() => setMessageId(m.id)}
                  className="block w-full py-3 text-left hover:bg-muted"
                >
                  <strong className="block truncate text-sm">
                    {m.subject || "（无主题）"}
                  </strong>
                  <span className="block truncate text-xs text-muted-foreground">
                    {m.sender}
                  </span>
                  <time className="text-xs text-muted-foreground">
                    {new Date(m.received_at).toLocaleString()}
                  </time>
                </button>
              ))}
            </div>
            {messages.data?.data.length === 0 && (
              <p className="text-sm text-muted-foreground">
                这个邮箱还没有邮件。
              </p>
            )}
            <div className="flex items-center gap-3">
              <Button
                variant="outline"
                disabled={page === 1}
                onClick={() => {
                  setPage(page - 1);
                  setMessageId("");
                }}
              >
                上一页
              </Button>
              <span>{page}</span>
              <Button
                variant="outline"
                disabled={page * 30 >= (messages.data?.meta?.total ?? 0)}
                onClick={() => {
                  setPage(page + 1);
                  setMessageId("");
                }}
              >
                下一页
              </Button>
            </div>
          </section>
          <section className="min-h-[560px]">
            {detail.error ? (
              <p role="alert" className="p-8">
                {companyError(detail.error)}
              </p>
            ) : detail.data?.data ? (
              <MessageDetail
                key={detail.data.data.id}
                message={detail.data.data}
                rawSource={null}
                rawSourceError="原文下载请使用已有受保护的源码接口；本工作台目前提供正文阅读。"
              />
            ) : (
              <p className="p-8 text-muted-foreground">
                选择一封邮件查看内容。
              </p>
            )}
          </section>
        </div>
      )}
      {tab === "compose" && (
        <section className="mx-auto w-full max-w-3xl space-y-5 p-6">
          {!senders.length ? (
            <p>当前账号没有发件权限。</p>
          ) : (
            <>
              <label className="block space-y-2">
                发件身份
                <select
                  aria-label="发件身份"
                  className={selectStyle}
                  value={sender?.address ?? ""}
                  onChange={(e) => {
                    setFrom(e.target.value);
                    setTemplateId("");
                    setVariables({});
                    resetPreview();
                  }}
                >
                  {senders.map((g) => (
                    <option key={g.mailbox_id} value={g.address}>
                      {g.address}
                    </option>
                  ))}
                </select>
              </label>
              <label className="block space-y-2">
                收件人
                <Textarea
                  aria-label="收件人"
                  value={recipients}
                  onChange={(e) => setRecipients(e.target.value)}
                  placeholder="填写邮件地址，多个地址用逗号或换行分隔"
                />
              </label>
              <label className="block space-y-2">
                邮件模板
                {required && (
                  <span className="ml-2 text-sm text-muted-foreground">
                    （必须使用）
                  </span>
                )}
                <select
                  aria-label="邮件模板"
                  className={selectStyle}
                  value={selected?.id ?? ""}
                  onChange={(e) => {
                    setTemplateId(e.target.value);
                    setVariables({});
                    resetPreview();
                  }}
                >
                  <option value="">
                    {required ? "请选择已发布模板" : "自由撰写"}
                  </option>
                  {available.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name} · v{t.version}
                    </option>
                  ))}
                </select>
              </label>
              {selected ? (
                <>
                  {Object.entries(selected.variables).map(([name, max]) => (
                    <label className="block space-y-2" key={name}>
                      {name}
                      <Input
                        aria-label={name}
                        value={variables[name] ?? ""}
                        maxLength={max}
                        onChange={(e) => {
                          setVariables({
                            ...variables,
                            [name]: e.target.value,
                          });
                          resetPreview();
                        }}
                      />
                    </label>
                  ))}
                  <p className="text-sm text-muted-foreground">
                    正文由服务器按固定版本生成，公司名称和员工姓名不可替换。
                  </p>
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={renderPreview}
                  >
                    预览模板邮件
                  </Button>
                </>
              ) : !required ? (
                <>
                  <label className="block space-y-2">
                    主题
                    <Input
                      aria-label="主题"
                      value={subject}
                      onChange={(e) => setSubject(e.target.value)}
                    />
                  </label>
                  <label className="block space-y-2">
                    正文
                    <Textarea
                      aria-label="正文"
                      className="min-h-52"
                      value={text}
                      onChange={(e) => setText(e.target.value)}
                    />
                  </label>
                </>
              ) : (
                <p>该身份只能通过模板发送。没有可用模板时，请联系管理员。</p>
              )}
              {preview && (
                <div className="rounded-lg border p-4">
                  <h2 className="font-semibold">{preview.subject}</h2>
                  <pre className="mt-3 whitespace-pre-wrap font-sans text-sm">
                    {preview.text_body}
                  </pre>
                  {preview.html_body && (
                    <iframe
                      title="模板 HTML 预览"
                      sandbox=""
                      className="mt-3 h-64 w-full border"
                      srcDoc={`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:">${preview.html_body}`}
                    />
                  )}
                </div>
              )}
              <Button
                disabled={
                  busy ||
                  Boolean(grants.error) ||
                  Boolean(templates.error) ||
                  (required && !selected)
                }
                onClick={send}
              >
                {busy ? "正在处理…" : "提交发送"}
              </Button>
            </>
          )}
        </section>
      )}
      {tab === "sent" && (
        <section className="space-y-4 p-6">
          <h2 className="font-semibold">最近 30 条本人发送记录</h2>
          <p className="text-sm text-muted-foreground">
            pending / retry 表示仍在队列中；sent
            表示投递服务器已接受，不代表对方已阅读。
          </p>
          {sent.error && <p role="alert">{companyError(sent.error)}</p>}
          <div className="divide-y">
            {sent.data?.data.map((job) => (
              <article className="py-3" key={job.id}>
                <span className="float-right rounded border px-2 text-xs">
                  {job.state}
                </span>
                <h3>{job.subject}</h3>
                <p className="text-sm text-muted-foreground">
                  {job.mail_from} → {job.rcpt_to.join(", ")}
                </p>
                {job.last_error && (
                  <p className="text-sm text-destructive">{job.last_error}</p>
                )}
              </article>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
