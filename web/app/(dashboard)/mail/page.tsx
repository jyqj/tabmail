"use client";
import { Suspense, useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { request, streamEvents } from "@/lib/api/base";
import type { APIError } from "@/lib/types";
import {
  company,
  workMailboxes,
  workMessages,
  workMessage,
  workPath,
  downloadCompanyFile,
  submissions,
  submission,
  submissionContent,
  submissionAttachments,
  type DraftPayload,
  type MailDraft,
  type WorkMailbox,
  type Submission,
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

const FOLDERS = ["inbox", "archive", "trash", "drafts", "sent"] as const;
type Folder = (typeof FOLDERS)[number];

export default function MailPage() {
  return (
    <Suspense
      fallback={
        <main className="mx-auto w-full max-w-7xl space-y-5 p-4 md:p-7">
          <p className="text-muted-foreground">Loading…</p>
        </main>
      }
    >
      <MailWorkbench />
    </Suspense>
  );
}

function MailWorkbench() {
  const t = useText();
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [selected, setSelected] = useState("");
  const [editor, setEditor] = useState<{
    draft: MailDraft;
    key: string;
  } | null>(null);

  // mailboxId / folder / search / page live in the URL query so refresh,
  // back/forward and deep links restore the exact workbench state. `selected`
  // stays a component-local detail view.
  const folderParam = searchParams.get("folder") ?? "";
  const folder: Folder = (FOLDERS as readonly string[]).includes(folderParam)
    ? (folderParam as Folder)
    : "inbox";
  const search = searchParams.get("q") ?? "";
  const pageRaw = Number.parseInt(searchParams.get("page") ?? "1", 10);
  const page = Number.isInteger(pageRaw) && pageRaw >= 1 ? pageRaw : 1;
  const mailboxParam = searchParams.get("mailbox") ?? "";
  // Keep URL canonical: drop values that equal the rendered defaults.
  const setQuery = (patch: Record<string, string | null>) => {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(patch)) {
      const drop =
        value === null ||
        value === "" ||
        (key === "folder" && value === "inbox") ||
        (key === "page" && value === "1");
      if (drop) params.delete(key);
      else params.set(key, value);
    }
    const qs = params.toString();
    router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
  };
  const boxes = useAPI("work-mailboxes", workMailboxes, {
    refreshInterval: 25000,
  });
  const mailbox =
    boxes.data?.find((v) => v.mailbox.id === mailboxParam) ?? boxes.data?.[0];
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
    folder === "sent" ? ["work-submissions", page] : null,
    () => submissions(page),
    { refreshInterval: 10000 },
  );
  const { busy, run } = useAction();
  const mutate = messages.mutate;
  const eventMailboxId = mailbox?.can_read ? mailbox.mailbox.id : undefined;
  useEffect(() => {
    if (!eventMailboxId) return;
    const abort = new AbortController();
    void streamEvents(
      `/api/v1/company/mailboxes/${encodeURIComponent(eventMailboxId)}/events`,
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
  }, [eventMailboxId, mutate]);
  // Composing needs a send-authorized identity: prefer the mailbox the user
  // is reading, otherwise the first sendable non-template-only mailbox, so a
  // reply draft is never silently dropped. The server re-authorizes the
  // chosen identity on every draft and submit call.
  function start(payload?: DraftPayload, mb = mailbox) {
    const from = mb?.can_send
      ? mb
      : boxes.data?.find((v) => v.can_send && !v.template_only);
    if (!from) return;
    setEditor({
      key: crypto.randomUUID(),
      draft: {
        mailbox_id: from.mailbox.id,
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
          onClick={() => start()}
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
            setQuery({ folder: "sent", page: null });
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
                      setQuery({ mailbox: e.target.value, page: null });
                      setSelected("");
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
            {FOLDERS.map((v) => (
              <ActionButton
                aria-pressed={folder === v}
                className={folder === v ? "bg-muted" : ""}
                key={v}
                onClick={() => {
                  setQuery({
                    folder: v === "inbox" ? null : v,
                    page: null,
                  });
                  setSelected("");
                }}
              >
                {label(v)}
              </ActionButton>
            ))}
          </nav>
          {["inbox", "archive", "trash"].includes(folder) && (
            <p className="text-sm text-muted-foreground">
              {t("范围：当前邮箱", "Scope: current mailbox")} ·{" "}
              {mailbox?.mailbox.full_address ?? "—"}
            </p>
          )}
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
                      setQuery({ q: e.target.value || null, page: null });
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
                            {m.starred ? "★ " : ""}
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
                  {selected && mailbox ? (
                    <MessagePane
                      key={`${mailbox.mailbox.id}:${selected}`}
                      mailbox={mailbox}
                      mailboxes={boxes.data ?? []}
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
              <p className="text-sm text-muted-foreground">
                {t(
                  "范围：我的全部草稿（跨邮箱），不受上方“当前邮箱”选择影响。",
                  "Scope: all my drafts across mailboxes — the current mailbox selector above does not apply.",
                )}
              </p>
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
                  "范围：我的提交，以及我有阅读权邮箱的提交（跨邮箱），不受上方“当前邮箱”选择影响。",
                  "Scope: my submissions and those from mailboxes I can read, across mailboxes — the current mailbox selector above does not apply.",
                )}
              </p>
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
              {(sent.data?.data ?? []).map((s) => (
                <div key={s.id} className="border-b py-3">
                  <button
                    className="w-full text-left"
                    onClick={() =>
                      setSelected(selected === s.id ? "" : s.id)
                    }
                  >
                    <p className="font-medium">
                      {s.subject || t("无主题", "No subject")}
                    </p>
                    <p className="text-sm">
                      {s.from} · <StatusBadge status={s.status} />
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {new Date(s.created_at).toLocaleString()} · {s.id}
                    </p>
                  </button>
                  {selected === s.id && <SubmissionPane id={s.id} />}
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
                  setQuery({ page: String(page - 1) });
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
                  setQuery({ page: String(page + 1) });
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

const STATUS_LABELS: Record<
  Submission["status"],
  { zh: string; en: string; className: string }
> = {
  submitted: {
    zh: "已入队",
    en: "Submitted",
    className: "bg-muted text-foreground",
  },
  waiting: {
    zh: "等待中",
    en: "Waiting",
    className: "bg-muted text-foreground",
  },
  sending: {
    zh: "发送中",
    en: "Sending",
    className: "bg-muted text-foreground",
  },
  partially_accepted: {
    zh: "部分已接受",
    en: "Partially accepted",
    className: "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200",
  },
  accepted: {
    zh: "下一跳已接受",
    en: "Accepted by next hop",
    className:
      "bg-green-100 text-green-900 dark:bg-green-950 dark:text-green-200",
  },
  needs_attention: {
    zh: "需要处理",
    en: "Needs attention",
    className: "bg-destructive text-destructive-foreground",
  },
};

function StatusBadge({ status }: { status: Submission["status"] }) {
  const t = useText();
  const s = STATUS_LABELS[status] ?? STATUS_LABELS.submitted;
  return (
    <span className={`rounded px-1.5 py-0.5 text-xs ${s.className}`}>
      {t(s.zh, s.en)}
    </span>
  );
}

const RECIPIENT_STATE_LABELS: Record<string, [string, string]> = {
  pending: ["待处理", "Pending"],
  accepted: ["已接受", "Accepted"],
  temporary: ["临时失败", "Temporary failure"],
  permanent: ["永久失败", "Permanent failure"],
  uncertain: ["结果不确定", "Uncertain"],
};

export function SubmissionPane({ id }: { id: string }) {
  const t = useText();
  const { busy, run } = useAction();
  const detail = useAPI(["submission", id], () => submission(id), {
    refreshInterval: 10000,
  });
  const [showContent, setShowContent] = useState(false);
  const s = detail.error ? undefined : detail.data;
  return (
    <div className="mt-4 space-y-3 rounded-md border p-4">
      <LoadError
        error={detail.error}
        onRetry={() => void detail.mutate()}
      />
      {s && (
        <>
          <p className="text-sm break-words">
            {s.from} →{" "}
            {(s.recipients ?? []).map((v) => v.address).join(", ") || "—"}
          </p>
          <p className="text-sm">
            <StatusBadge status={s.status} />
          </p>
          <p className="text-xs text-muted-foreground">
            {t("附件", "Attachments")}: {s.attachment_count}
            {s.draft_consumed
              ? ` · ${t("来自草稿", "from a draft")}`
              : ""}
            {s.template_version_id
              ? ` · ${t("模板版本", "Template version")}: ${s.template_version_id}`
              : ""}
          </p>
          {/* capabilities is an interaction hint, not a credential; absent on
              stale cached data keeps the current behavior. The content
              endpoint re-checks read access on every call. */}
          {s.capabilities?.view_content !== false && (
            <div className="flex gap-2">
              <ActionButton
                aria-expanded={showContent}
                onClick={() => setShowContent((v) => !v)}
              >
                {showContent
                  ? t("收起内容", "Hide content")
                  : t("查看内容", "View content")}
              </ActionButton>
            </div>
          )}
          {showContent && <SubmissionContentView id={id} />}
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr>
                  <th>{t("收件人", "Recipient")}</th>
                  <th>{t("结果", "Outcome")}</th>
                </tr>
              </thead>
              <tbody>
                {(s.recipients ?? []).map((v) => {
                  const l = RECIPIENT_STATE_LABELS[v.state] ?? [
                    v.state,
                    v.state,
                  ];
                  return (
                    <tr key={v.address}>
                      <td className="py-2 break-all">{v.address}</td>
                      <td>{t(l[0], l[1])}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {/* capabilities.retry is the interaction hint; when absent (old
              cached data) fall back to the status heuristic. The retry POST
              re-authorizes server-side either way. */}
          {(s.capabilities
            ? s.capabilities.retry
            : s.status === "needs_attention") && (
            <ActionButton
              disabled={busy}
              onClick={() =>
                run(async () => {
                  try {
                    await request(`/api/v1/outbound/${id}/retry`, {
                      method: "POST",
                      body: {},
                    });
                    toast.success(
                      t("已请求安全重试", "Safe retry requested"),
                    );
                    void detail.mutate();
                  } catch (e) {
                    if ((e as APIError)?.error?.code === "CONFLICT") {
                      const reason = (e as APIError)?.error?.reason;
                      toast.error(
                        reason === "state_changed"
                          ? t(
                              "任务状态已变化，请刷新后查看。",
                              "The task state has changed; refresh to see the latest status.",
                            )
                          : t(
                              "结果不确定，已禁止重试。请到恢复中心核实下一跳记录后再处理。",
                              "Uncertain outcome: retry is blocked. Review next-hop evidence in the recovery center.",
                            ),
                      );
                      return;
                    }
                    throw e;
                  }
                })
              }
            >
              {t(
                "重试未成功目标（服务端重新鉴权）",
                "Retry unfinished recipients (re-authorized by server)",
              )}
            </ActionButton>
          )}
          {s.delivery_uncertain && (
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

// SubmissionContentView renders the actual sent body and the pinned
// attachments of one submission. It reuses the message pane's safe rendering
// conventions: plain text in a pre, HTML only through the sandboxed MailHTML
// view, and downloads through the per-submission server endpoint (storage
// keys never reach the client).
function SubmissionContentView({ id }: { id: string }) {
  const t = useText();
  const { busy, run } = useAction();
  const content = useAPI(["submission-content", id], () =>
    submissionContent(id),
  );
  const files = useAPI(["submission-attachments", id], () =>
    submissionAttachments(id),
  );
  const c = content.error ? undefined : content.data;
  return (
    <div className="space-y-3 border-t pt-3">
      <LoadError
        error={content.error}
        onRetry={() => void content.mutate()}
      />
      {content.isLoading && (
        <p className="text-sm text-muted-foreground">
          {t("加载中…", "Loading…")}
        </p>
      )}
      {c && (
        <>
          {c.content_redacted ? (
            <p role="status" className="text-sm">
              {t("正文内容对你不可见。", "The message body is not visible to you.")}
            </p>
          ) : (
            <>
              {c.text_body && (
                <pre className="whitespace-pre-wrap break-words text-sm leading-7">
                  {c.text_body}
                </pre>
              )}
              {c.html_body && (
                <details>
                  <summary className="cursor-pointer">
                    {t(
                      "安全 HTML 视图（外部资源已阻止）",
                      "Safe HTML view (external resources blocked)",
                    )}
                  </summary>
                  <MailHTML html={c.html_body} />
                </details>
              )}
            </>
          )}
        </>
      )}
      <LoadError error={files.error} onRetry={() => void files.mutate()} />
      {(files.data ?? []).map((f) => (
        <ActionButton
          key={f.id}
          disabled={busy}
          onClick={() =>
            run(() =>
              downloadCompanyFile(
                `/submissions/${encodeURIComponent(id)}/attachments/${encodeURIComponent(f.id)}/download`,
                f.filename,
              ),
            )
          }
        >
          {f.filename} · {Math.ceil(f.size / 1024)} KiB
        </ActionButton>
      ))}
    </div>
  );
}

export function MessagePane({
  mailbox,
  mailboxes,
  id,
  onMutation,
  onCompose,
}: {
  mailbox: WorkMailbox;
  /** Send-identity data source; defaults to the current mailbox only. */
  mailboxes?: WorkMailbox[];
  id: string;
  onMutation: () => void;
  onCompose: (p: DraftPayload) => void;
}) {
  const t = useText();
  const { busy, run } = useAction();
  // Reply/forward only needs some sendable identity to exist — the compose
  // view owns the identity switch. The server re-authorizes every send.
  const canCompose = (mailboxes ?? [mailbox]).some((v) => v.can_send);
  // Same default-identity rule as start(): prefer the reading mailbox, else
  // the first sendable non-template-only mailbox. The server re-authorizes.
  const composeFrom =
    (mailbox.can_send && mailbox) ||
    (mailboxes ?? [mailbox]).find((v) => v.can_send && !v.template_only) ||
    mailbox;
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
            {detail.data.starred ? "★ " : ""}
            {detail.data.sender} → {(detail.data.recipients ?? []).join(", ")}
          </p>
          <div className="flex flex-wrap gap-2">
            {(["reply", "reply_all", "forward"] as const).map((mode, i) => (
              <ActionButton
                key={mode}
                disabled={busy || !canCompose}
                onClick={() =>
                  run(async () =>
                    onCompose(
                      await company<DraftPayload>(`${base}/compose`, {
                        method: "POST",
                        body: { mode, from_mailbox_id: composeFrom.mailbox.id },
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
          {/* Personal per-user state: any reader may toggle seen/starred.
              The server still re-authorizes every action call. */}
          {mailbox.can_read && (
            <div className="flex flex-wrap gap-2 border-t pt-3">
              <ActionButton
                disabled={busy}
                onClick={() => run(() => act(detail.data!.seen ? "unseen" : "seen"))}
              >
                {detail.data.seen
                  ? t("标为未读", "Mark unread")
                  : t("标为已读", "Mark read")}
              </ActionButton>
              <ActionButton
                disabled={busy}
                onClick={() =>
                  run(() => act(detail.data!.starred ? "unstarred" : "starred"))
                }
              >
                {detail.data.starred
                  ? t("取消星标", "Unstar")
                  : t("加星标", "Star")}
              </ActionButton>
            </div>
          )}
          {/* Shared-folder organizing (archive/trash/restore) stays behind
              the organizer grant. */}
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
