"use client";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { request, streamEvents } from "@/lib/api/base";
import type { APIListResponse, APIResponse, OutboundJob } from "@/lib/types";
import {
  company,
  workMailboxes,
  workMessages,
  workMessage,
  workPath,
  downloadCompanyFile,
  type DraftPayload,
  type MailDraft,
  type WorkMailbox,
  type RecipientResult,
  type InboundAttachment,
} from "@/lib/company";
import {
  ActionButton,
  Field,
  inputClass,
  LoadError,
  MailHTML,
  Section,
  useAction,
  useText,
} from "@/components/company/common";
import { Compose } from "@/components/company/compose";

type Folder = "inbox" | "archive" | "trash" | "drafts" | "sent";
export default function MailPage() {
  const t = useText();
  const [mailboxId, setMailboxId] = useState("");
  const [folder, setFolder] = useState<Folder>("inbox");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState("");
  const [editor, setEditor] = useState<{
    draft: MailDraft;
    key: string;
  } | null>(null);
  const boxes = useAPI("work-mailboxes", workMailboxes, {
    refreshInterval: 25000,
  });
  const mailbox =
    boxes.data?.find((v) => v.mailbox.id === mailboxId) ?? boxes.data?.[0];
  const messages = useAPI(
    mailbox?.can_read && ["inbox", "archive", "trash"].includes(folder)
      ? ["work-messages", mailbox.mailbox.id, folder, search, page]
      : null,
    () => workMessages(mailbox!.mailbox.id, folder, search, page),
    { refreshInterval: 25000 },
  );
  const drafts = useAPI(folder === "drafts" ? "work-drafts" : null, () =>
    company<MailDraft[]>("/drafts"),
  );
  const sent = useAPI(
    folder === "sent" ? ["work-sent", page] : null,
    () =>
      request<APIListResponse<OutboundJob>>("/api/v1/outbound", {
        params: { page, per_page: 30 },
      }),
    { refreshInterval: 10000 },
  );
  const { busy, run } = useAction();
  const mutate = messages.mutate;
  const eventAddress = mailbox?.can_read
    ? mailbox.mailbox.full_address
    : undefined;
  useEffect(() => {
    if (!eventAddress) return;
    const abort = new AbortController();
    void streamEvents(
      `/api/v1/mailbox/${encodeURIComponent(eventAddress)}/events`,
      {
        signal: abort.signal,
        onEvent: (event) => {
          if (event.type !== "ping") void mutate();
        },
      },
    ).catch(() => {
      /* Polling remains the authoritative convergence fallback. */
    });
    return () => abort.abort();
  }, [eventAddress, mutate]);
  function start(payload?: DraftPayload, mb = mailbox) {
    if (!mb?.can_send) return;
    setEditor({
      key: crypto.randomUUID(),
      draft: {
        mailbox_id: mb.mailbox.id,
        revision: 0,
        payload: payload ?? { to: [], subject: "", text_body: "" },
      },
    });
  }
  const label = (v: Folder) =>
    ({
      inbox: t("收件箱", "Inbox"),
      archive: t("归档", "Archive"),
      trash: t("回收站", "Trash"),
      drafts: t("草稿", "Drafts"),
      sent: t("已提交 / 发件", "Submitted / Sent"),
    })[v];
  const total =
    folder === "sent" ? sent.data?.meta.total : messages.data?.meta.total;
  return (
    <main className="mx-auto w-full max-w-7xl space-y-5 p-4 md:p-7">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">
            {t("公司邮箱", "Company mail")}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {t(
              "个人与共享邮箱 · 当前授权决定可见内容",
              "Personal and shared mailboxes · access follows current permissions",
            )}
          </p>
        </div>
        <ActionButton
          disabled={Boolean(editor) || !boxes.data?.some((v) => v.can_send)}
          onClick={() =>
            start(
              undefined,
              mailbox?.can_send ? mailbox : boxes.data?.find((v) => v.can_send),
            )
          }
        >
          {t("写邮件", "Compose")}
        </ActionButton>
      </header>
      <LoadError error={boxes.error} onRetry={() => void boxes.mutate()} />
      {editor ? (
        <Compose
          key={editor.key}
          mailboxes={boxes.data ?? []}
          initial={editor.draft}
          onClose={() => setEditor(null)}
          onSent={() => {
            setEditor(null);
            setFolder("sent");
            setSelected("");
            void sent.mutate();
            void drafts.mutate();
          }}
        />
      ) : (
        <>
          <div className="flex flex-wrap gap-4 items-end">
            <div className="min-w-64 flex-1">
              <Field label={t("当前邮箱", "Current mailbox")}>
                {(id) => (
                  <select
                    id={id}
                    className={inputClass}
                    value={mailbox?.mailbox.id ?? ""}
                    onChange={(e) => {
                      setMailboxId(e.target.value);
                      setSelected("");
                      setPage(1);
                    }}
                  >
                    {(boxes.data ?? []).map((v) => (
                      <option key={v.mailbox.id} value={v.mailbox.id}>
                        {v.mailbox.full_address} · {v.mailbox.kind ?? "legacy"}
                        {!v.can_read
                          ? t("（不可阅读）", " (no read access)")
                          : ""}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
            </div>
            <ActionButton
              disabled={busy}
              onClick={() => {
                void boxes.mutate();
                void messages.mutate();
                void sent.mutate();
                void drafts.mutate();
              }}
            >
              {t("刷新", "Refresh")}
            </ActionButton>
          </div>
          {!boxes.isLoading && !boxes.data?.length && (
            <p role="status">
              {t(
                "尚未分配邮箱，请联系公司管理员。",
                "No mailbox assigned. Contact your company administrator.",
              )}
            </p>
          )}
          <nav
            aria-label={t("邮件文件夹", "Mail folders")}
            className="flex flex-wrap gap-2"
          >
            {(["inbox", "archive", "trash", "drafts", "sent"] as Folder[]).map(
              (v) => (
                <ActionButton
                  aria-pressed={folder === v}
                  className={folder === v ? "bg-muted" : ""}
                  key={v}
                  onClick={() => {
                    setFolder(v);
                    setSelected("");
                    setPage(1);
                  }}
                >
                  {label(v)}
                </ActionButton>
              ),
            )}
          </nav>
          {folder === "trash" && (
            <p className="text-sm text-muted-foreground">
              {t(
                "删除的邮件保留 30 天，可恢复。到期清理后不能通过此界面找回。",
                "Deleted messages can be restored for 30 days, until retention cleanup.",
              )}
            </p>
          )}
          {["inbox", "archive", "trash"].includes(folder) && (
            <>
              <Field
                label={t(
                  "搜索主题、发件人或收件地址",
                  "Search subject, sender or recipient",
                )}
              >
                {(id) => (
                  <input
                    id={id}
                    className={inputClass}
                    value={search}
                    maxLength={200}
                    onChange={(e) => {
                      setSearch(e.target.value);
                      setPage(1);
                      setSelected("");
                    }}
                  />
                )}
              </Field>
              <LoadError
                error={messages.error}
                onRetry={() => void messages.mutate()}
              />
              {!mailbox?.can_read ? (
                <p role="status">
                  {t(
                    "你没有此邮箱的阅读权限。代发权限不包含阅读权限。",
                    "You cannot read this mailbox. Send-as does not imply read access.",
                  )}
                </p>
              ) : (
                <div className="grid gap-5 lg:grid-cols-[minmax(260px,1fr)_minmax(0,2fr)]">
                  <section
                    className="rounded-lg border overflow-hidden"
                    aria-label={label(folder)}
                  >
                    {messages.isLoading ? (
                      <p className="p-4">{t("加载中…", "Loading…")}</p>
                    ) : !messages.data?.data?.length ? (
                      <p className="p-4 text-muted-foreground">
                        {t("没有邮件", "No messages")}
                      </p>
                    ) : (
                      messages.data.data.map((m) => (
                        <button
                          key={m.id}
                          className={`w-full border-b p-4 text-left hover:bg-muted ${selected === m.id ? "bg-muted" : ""}`}
                          onClick={() => setSelected(m.id)}
                        >
                          <p
                            className={`truncate ${m.seen ? "" : "font-semibold"}`}
                          >
                            {m.subject || t("无主题", "No subject")}
                          </p>
                          <p className="mt-1 truncate text-xs text-muted-foreground">
                            {m.sender}
                          </p>
                          <p className="mt-1 text-xs text-muted-foreground">
                            {new Date(m.received_at).toLocaleString()}
                          </p>
                        </button>
                      ))
                    )}
                  </section>
                  {selected ? (
                    <MessagePane
                      key={`${mailbox.mailbox.id}:${selected}`}
                      mailbox={mailbox}
                      id={selected}
                      onMutation={() => {
                        setSelected("");
                        void messages.mutate();
                      }}
                      onCompose={(p) => start(p)}
                    />
                  ) : (
                    <div className="rounded-lg border border-dashed p-10 text-center text-muted-foreground">
                      {t("选择一封邮件阅读", "Select a message")}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
          {folder === "drafts" && (
            <Section title={label(folder)}>
              <LoadError
                error={drafts.error}
                onRetry={() => void drafts.mutate()}
              />
              {(drafts.data ?? []).map((d) => (
                <div
                  className="flex flex-wrap justify-between gap-3 border-b pb-3"
                  key={d.id}
                >
                  <div>
                    <p className="font-medium">
                      {d.payload.subject ||
                        t("模板邮件 / 无主题", "Template / no subject")}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {
                        boxes.data?.find((v) => v.mailbox.id === d.mailbox_id)
                          ?.mailbox.full_address
                      }{" "}
                      · v{d.revision}
                    </p>
                  </div>
                  <div className="flex gap-2">
                    <ActionButton
                      onClick={() =>
                        setEditor({ draft: d, key: crypto.randomUUID() })
                      }
                    >
                      {t("继续编辑", "Continue editing")}
                    </ActionButton>
                    <ActionButton
                      disabled={busy}
                      onClick={() =>
                        run(async () => {
                          await company(`/drafts/${d.id}`, {
                            method: "DELETE",
                            params: { revision: d.revision },
                          });
                          void drafts.mutate();
                        })
                      }
                    >
                      {t("删除草稿", "Delete draft")}
                    </ActionButton>
                  </div>
                </div>
              ))}
              {!drafts.isLoading && !drafts.data?.length && (
                <p>{t("没有草稿", "No drafts")}</p>
              )}
            </Section>
          )}
          {folder === "sent" && (
            <Section title={label(folder)}>
              <p className="text-sm text-muted-foreground">
                {t(
                  "“下一跳已接受”不代表收件人已收到或阅读。不确定的投递需要运维核查，不能盲目重发。",
                  "Accepted by next hop does not mean delivered to the inbox or read. Uncertain outcomes require operator review.",
                )}
              </p>
              <LoadError
                error={sent.error}
                onRetry={() => void sent.mutate()}
              />
              {(sent.data?.data ?? []).map((j) => (
                <div key={j.id} className="border-b py-3">
                  <button
                    className="w-full text-left"
                    onClick={() => setSelected(selected === j.id ? "" : j.id)}
                  >
                    <p className="font-medium">{j.subject}</p>
                    <p className="text-sm">
                      {j.mail_from} ·{" "}
                      {j.delivery_uncertain
                        ? t("结果不确定", "Uncertain")
                        : j.state === "sent"
                          ? t("下一跳已接受", "Accepted by next hop")
                          : j.state}
                    </p>
                    <p className="text-xs text-muted-foreground">{j.id}</p>
                  </button>
                  {selected === j.id && <SentPane id={j.id} />}
                </div>
              ))}
              {!sent.isLoading && !sent.data?.data.length && (
                <p>{t("没有发送记录", "No submissions")}</p>
              )}
            </Section>
          )}
          {folder !== "drafts" && (
            <div className="flex justify-center items-center gap-4">
              <ActionButton
                disabled={page <= 1}
                onClick={() => {
                  setPage((v) => v - 1);
                  setSelected("");
                }}
              >
                {t("上一页", "Previous")}
              </ActionButton>
              <span className="text-sm">
                {page} / {Math.max(1, Math.ceil((total ?? 0) / 30))}
              </span>
              <ActionButton
                disabled={page * 30 >= (total ?? 0)}
                onClick={() => {
                  setPage((v) => v + 1);
                  setSelected("");
                }}
              >
                {t("下一页", "Next")}
              </ActionButton>
            </div>
          )}
        </>
      )}
    </main>
  );
}
function MessagePane({
  mailbox,
  id,
  onMutation,
  onCompose,
}: {
  mailbox: WorkMailbox;
  id: string;
  onMutation: () => void;
  onCompose: (p: DraftPayload) => void;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const detail = useAPI(["work-message", mailbox.mailbox.id, id], () =>
    workMessage(mailbox.mailbox.id, id),
  );
  const base = `${workPath(mailbox.mailbox.id)}/messages/${id}`;
  const files = useAPI(["inbound-attachments", mailbox.mailbox.id, id], () =>
    company<InboundAttachment[]>(`${base}/attachments`),
  );
  async function act(action: string) {
    await company(`${base}/actions`, { method: "POST", body: { action } });
    onMutation();
  }
  return (
    <Section title={detail.data?.subject || t("邮件详情", "Message details")}>
      <LoadError error={detail.error} onRetry={() => void detail.mutate()} />
      {detail.data && !detail.error && (
        <>
          <p className="text-sm break-words">
            {detail.data.sender} → {(detail.data.recipients ?? []).join(", ")}
          </p>
          <div className="flex flex-wrap gap-2">
            {(["reply", "reply_all", "forward"] as const).map((mode, i) => (
              <ActionButton
                key={mode}
                disabled={busy || !mailbox.can_send}
                onClick={() =>
                  run(async () =>
                    onCompose(
                      await company<DraftPayload>(`${base}/compose`, {
                        method: "POST",
                        body: { mode, from_mailbox_id: mailbox.mailbox.id },
                      }),
                    ),
                  )
                }
              >
                {
                  [
                    t("回复", "Reply"),
                    t("回复全部", "Reply all"),
                    t("转发", "Forward"),
                  ][i]
                }
              </ActionButton>
            ))}
            <ActionButton
              disabled={busy}
              onClick={() =>
                run(() => downloadCompanyFile(`${base}/source`, "message.eml"))
              }
            >
              {t("下载原件", "Download original")}
            </ActionButton>
          </div>
          <pre className="whitespace-pre-wrap break-words text-sm leading-7">
            {detail.data.text_body}
          </pre>
          {detail.data.html_body && (
            <details>
              <summary className="cursor-pointer">
                {t(
                  "安全 HTML 视图（外部资源已阻止）",
                  "Safe HTML view (external resources blocked)",
                )}
              </summary>
              <MailHTML html={detail.data.html_body} />
            </details>
          )}
          <LoadError error={files.error} onRetry={() => void files.mutate()} />
          {(files.data ?? []).map((f) => (
            <ActionButton
              key={f.index}
              disabled={busy}
              onClick={() =>
                run(() =>
                  downloadCompanyFile(
                    `${base}/attachments/${f.index}`,
                    f.filename,
                  ),
                )
              }
            >
              {f.filename} · {Math.ceil(f.size / 1024)} KiB
            </ActionButton>
          ))}
          {mailbox.can_organize && (
            <div className="flex flex-wrap gap-2 border-t pt-3">
              {detail.data.deleted_at ? (
                <ActionButton
                  disabled={busy}
                  onClick={() => run(() => act("restore"))}
                >
                  {t("恢复邮件", "Restore")}
                </ActionButton>
              ) : (
                <>
                  <ActionButton
                    disabled={busy}
                    onClick={() =>
                      run(() =>
                        act(detail.data!.archived_at ? "unarchive" : "archive"),
                      )
                    }
                  >
                    {detail.data.archived_at
                      ? t("移回收件箱", "Move to inbox")
                      : t("归档", "Archive")}
                  </ActionButton>
                  <ActionButton
                    disabled={busy}
                    onClick={() =>
                      run(() => act(detail.data!.seen ? "unseen" : "seen"))
                    }
                  >
                    {detail.data.seen
                      ? t("标为未读", "Mark unread")
                      : t("标为已读", "Mark read")}
                  </ActionButton>
                  <ActionButton
                    disabled={busy}
                    onClick={() => run(() => act("trash"))}
                  >
                    {t("移到回收站", "Move to trash")}
                  </ActionButton>
                </>
              )}
            </div>
          )}
        </>
      )}
    </Section>
  );
}
function SentPane({ id }: { id: string }) {
  const t = useText();
  const { busy, run } = useAction();
  const detail = useAPI(
    ["sent-detail", id],
    () =>
      request<APIResponse<OutboundJob>>(`/api/v1/outbound/${id}`).then(
        (r) => r.data,
      ),
    { refreshInterval: 10000 },
  );
  const recipients = useAPI(
    ["recipient-results", id],
    () => company<RecipientResult[]>(`/outbound/${id}/recipients`),
    { refreshInterval: 10000 },
  );
  const j = detail.error ? undefined : detail.data;
  return (
    <div className="mt-4 space-y-3 rounded-md border p-4">
      <LoadError
        error={detail.error || recipients.error}
        onRetry={() => {
          void detail.mutate();
          void recipients.mutate();
        }}
      />
      {j && (
        <>
          {j.content_redacted ? (
            <p>
              {t(
                "你只有运行元数据权限，正文与密送信息已隐藏。",
                "Content and Bcc are hidden; you only have operational metadata access.",
              )}
            </p>
          ) : (
            <>
              <pre className="whitespace-pre-wrap text-sm">{j.text_body}</pre>
              {j.html_body && <MailHTML html={j.html_body} />}
              <p className="text-xs">
                {t("模板版本", "Template version")}:{" "}
                {j.template_version_id ?? "—"}
              </p>
              {(j.attachment_ids ?? []).map((a, i) => (
                <ActionButton
                  disabled={busy}
                  key={a}
                  onClick={() =>
                    run(() =>
                      downloadCompanyFile(
                        `/attachments/${a}`,
                        `attachment-${i + 1}`,
                      ),
                    )
                  }
                >
                  {t("附件", "Attachment")} {i + 1}
                </ActionButton>
              ))}
            </>
          )}
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr>
                  <th>{t("收件人", "Recipient")}</th>
                  <th>{t("结果", "Outcome")}</th>
                  <th>SMTP</th>
                </tr>
              </thead>
              <tbody>
                {(recipients.data ?? []).map((v) => (
                  <tr key={v.address}>
                    <td className="py-2 break-all">{v.address}</td>
                    <td>
                      {v.state}
                      {v.diagnostic && (
                        <p className="max-w-sm text-xs text-muted-foreground break-words">
                          {v.diagnostic}
                        </p>
                      )}
                    </td>
                    <td>{v.smtp_code || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {j.last_error && (
            <p className="text-sm text-destructive break-words">
              {j.last_error}
            </p>
          )}
          {["failed", "dead", "retry"].includes(j.state) && (
            <ActionButton
              disabled={busy || j.delivery_uncertain || j.content_redacted}
              onClick={() =>
                run(async () => {
                  await request(`/api/v1/outbound/${id}/retry`, {
                    method: "POST",
                    body: {},
                  });
                  toast.success(t("已请求安全重试", "Safe retry requested"));
                  void detail.mutate();
                  void recipients.mutate();
                })
              }
            >
              {t(
                "重试未成功目标（服务端重新鉴权）",
                "Retry unfinished recipients (re-authorized by server)",
              )}
            </ActionButton>
          )}
          {j.delivery_uncertain && (
            <p role="status" className="text-sm">
              {t(
                "结果不确定，已禁止重试。请联系平台运维核实下一跳记录后再处理。",
                "Uncertain outcome: retry is blocked. Ask an operator to verify next-hop evidence.",
              )}
            </p>
          )}
        </>
      )}
    </div>
  );
}
