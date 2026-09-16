"use client";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { request } from "@/lib/api/base";
import type { APIResponse, SendEmailResponse } from "@/lib/types";
import {
  addresses,
  company,
  downloadCompanyFile,
  workPath,
  type MailAttachment,
  type MailDraft,
  type DraftPayload,
  type RenderedTemplate,
  type TemplateVersion,
  type WorkMailbox,
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
} from "./common";

export function Compose({
  mailboxes,
  initial,
  onClose,
  onSent,
}: {
  mailboxes: WorkMailbox[];
  initial: MailDraft;
  onClose: () => void;
  onSent: () => void;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const [draft, setDraft] = useState(initial);
  const [payload, setPayload] = useState<DraftPayload>(initial.payload);
  const [mailboxId, setMailboxId] = useState(initial.mailbox_id);
  const [names, setNames] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<RenderedTemplate | null>(null);
  const [pending, setPending] = useState<{ key: string; body: unknown } | null>(
    null,
  );
  const nonce = useRef("");
  const from = mailboxes.find((v) => v.mailbox.id === mailboxId);
  const templates = useAPI(["usable-templates", mailboxId], () =>
    company<TemplateVersion[]>(`${workPath(mailboxId)}/templates`),
  );
  const version = templates.data?.find(
    (v) => v.id === payload.template_version_id,
  );
  const invalidVersion = Boolean(payload.template_version_id && !version);
  const locked = busy || Boolean(pending);
  const dirty =
    JSON.stringify(payload) !== JSON.stringify(draft.payload) ||
    draft.mailbox_id !== mailboxId;
  useEffect(() => {
    if (!dirty && !pending) return;
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty, pending]);
  function change(patch: Partial<DraftPayload>) {
    setPayload((v) => ({ ...v, ...patch }));
    setPreview(null);
  }
  async function save() {
    const value = await company<MailDraft>(
      draft.id ? `/drafts/${draft.id}` : "/drafts",
      {
        method: draft.id ? "PUT" : "POST",
        body: {
          id: draft.id,
          mailbox_id: mailboxId,
          payload,
          revision: draft.revision,
        },
      },
    );
    setDraft(value);
    setPayload(value.payload);
    toast.success(t("草稿已保存", "Draft saved"));
    return value;
  }
  async function send() {
    if (!from?.can_send) return;
    if (!pending && (invalidVersion || (from.template_only && !version)))
      throw new Error(
        t(
          "请选择当前获准使用的已发布模板",
          "Select an authorized published template",
        ),
      );
    const body = {
      from: from.mailbox.full_address,
      to: payload.to,
      cc: payload.cc ?? [],
      bcc: payload.bcc ?? [],
      subject: payload.subject,
      text_body: payload.text_body,
      html_body: payload.html_body ?? "",
      headers: payload.headers ?? {},
      template_version_id: payload.template_version_id,
      template_vars: payload.template_vars ?? {},
      attachment_ids: payload.attachment_ids ?? [],
    };
    let snapshot = pending;
    if (!snapshot) {
      if (!nonce.current) nonce.current = crypto.randomUUID();
      let key = nonce.current;
      if (draft.id) {
        const bytes = await crypto.subtle.digest(
          "SHA-256",
          new TextEncoder().encode(JSON.stringify(body)),
        );
        key = `${draft.id}.${draft.revision}.${Array.from(new Uint8Array(bytes))
          .map((b) => b.toString(16).padStart(2, "0"))
          .join("")}`;
      }
      snapshot = { key, body };
      setPending(snapshot);
    }
    try {
      const res = await request<APIResponse<SendEmailResponse>>(
        "/api/v1/send",
        {
          method: "POST",
          headers: { "Idempotency-Key": snapshot.key },
          body: snapshot.body,
        },
      );
      if (draft.id) {
        try {
          await company(`/drafts/${draft.id}`, {
            method: "DELETE",
            params: { revision: draft.revision },
          });
        } catch {
          toast.warning(
            t(
              "邮件已入队，但草稿清理失败，请在草稿箱核对",
              "Mail queued; draft cleanup failed. Review the draft list.",
            ),
          );
        }
      }
      toast.success(
        `${t("已加入发送队列，不代表已送达：", "Queued, not yet delivered: ")}${res.data.id}`,
      );
      onSent();
    } catch (e) {
      const code = (e as { error?: { code?: string } })?.error?.code;
      if (
        [
          "BAD_REQUEST",
          "FORBIDDEN",
          "QUOTA_EXCEEDED",
          "CONFLICT",
          "UNAUTHORIZED",
        ].includes(code ?? "")
      )
        setPending(null);
      throw e;
    }
  }
  return (
    <Section title={t("撰写邮件", "Compose mail")}>
      <div className="flex justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {t(
            "发件身份由服务器授权；共享邮箱的阅读权与代发权互相独立。",
            "Sender identities are authorized by the server; read and send-as rights are separate.",
          )}
        </p>
        <ActionButton
          disabled={busy}
          onClick={() => {
            if (
              (dirty || pending) &&
              !window.confirm(
                t(
                  "离开将丢弃未保存的编辑。提交结果未确认时请先检查发件箱。继续？",
                  "Discard unsaved edits? Check Sent first when submission is uncertain.",
                ),
              )
            )
              return;
            onClose();
          }}
        >
          {t("关闭", "Close")}
        </ActionButton>
      </div>
      {pending && (
        <p
          role="status"
          className="rounded border border-amber-500 p-3 text-sm"
        >
          {t(
            "正在提交或提交结果尚未确认。内容暂时锁定；重试使用同一幂等键，不会创建重复任务。",
            "Submission pending or unconfirmed. Content is locked; retry reuses the same idempotency key.",
          )}
        </p>
      )}
      <Field label={t("发件邮箱", "From mailbox")}>
        {(id) => (
          <select
            id={id}
            className={inputClass}
            value={mailboxId}
            disabled={locked}
            onChange={(e) => {
              setMailboxId(e.target.value);
              change({
                attachment_ids: [],
                template_version_id: undefined,
                template_vars: {},
              });
            }}
          >
            {mailboxes
              .filter((v) => v.can_send)
              .map((v) => (
                <option key={v.mailbox.id} value={v.mailbox.id}>
                  {v.mailbox.full_address}
                  {v.template_only ? t(" · 仅模板", " · template only") : ""}
                </option>
              ))}
          </select>
        )}
      </Field>
      <div className="grid gap-4 md:grid-cols-3">
        {(["to", "cc", "bcc"] as const).map((field, i) => (
          <Field
            key={field}
            label={
              [
                t("收件人（纯邮箱地址）", "To (plain email addresses)"),
                t("抄送", "Cc"),
                t("密送", "Bcc"),
              ][i]
            }
          >
            {(id) => (
              <RecipientInput
                id={id}
                disabled={locked}
                value={payload[field] ?? []}
                onChange={(value) => change({ [field]: value })}
              />
            )}
          </Field>
        ))}
      </div>
      <LoadError
        error={templates.error}
        onRetry={() => void templates.mutate()}
      />
      <Field label={t("已发布模板", "Published template")}>
        {(id) => (
          <select
            id={id}
            className={inputClass}
            disabled={locked || templates.isLoading}
            value={payload.template_version_id ?? ""}
            onChange={(e) =>
              change({
                template_version_id: e.target.value || undefined,
                template_vars: {},
              })
            }
          >
            <option value="">
              {from?.template_only
                ? t("必须选择模板", "Template required")
                : t("自由撰写", "Free composition")}
            </option>
            {invalidVersion && (
              <option value={payload.template_version_id}>
                {t(
                  "此版本不可用，请重新选择",
                  "Version unavailable; select another",
                )}
              </option>
            )}
            {(templates.data ?? []).map((v) => (
              <option key={v.id} value={v.id}>
                {v.name} · v{v.version}
              </option>
            ))}
          </select>
        )}
      </Field>
      {version ? (
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">
            {t(
              "姓名、公司名和发件地址由服务器填充。最终内容来自这个固定版本。",
              "Employee, company and sender identity are filled by the server. This version is immutable.",
            )}
          </p>
          {version.snapshot.variables.map((v) => (
            <Field
              key={v.name}
              label={`${v.name}${v.required ? " *" : ""} · ${v.type}`}
            >
              {(id) =>
                v.options?.length ? (
                  <select
                    id={id}
                    className={inputClass}
                    disabled={locked}
                    value={payload.template_vars?.[v.name] ?? ""}
                    onChange={(e) =>
                      change({
                        template_vars: {
                          ...payload.template_vars,
                          [v.name]: e.target.value,
                        },
                      })
                    }
                  >
                    <option value="" />
                    {v.options.map((o) => (
                      <option key={o}>{o}</option>
                    ))}
                  </select>
                ) : (
                  <textarea
                    id={id}
                    className={inputClass}
                    disabled={locked}
                    required={v.required}
                    maxLength={v.max_length}
                    rows={v.type === "text" ? 3 : 1}
                    value={payload.template_vars?.[v.name] ?? ""}
                    onChange={(e) =>
                      change({
                        template_vars: {
                          ...payload.template_vars,
                          [v.name]: e.target.value,
                        },
                      })
                    }
                  />
                )
              }
            </Field>
          ))}
          <ActionButton
            disabled={busy || Boolean(pending)}
            onClick={() =>
              run(async () =>
                setPreview(
                  await company<RenderedTemplate>("/templates/preview", {
                    method: "POST",
                    body: {
                      mailbox_id: mailboxId,
                      template_version_id: version.id,
                      vars: payload.template_vars ?? {},
                    },
                  }),
                ),
              )
            }
          >
            {t("服务端预览", "Server preview")}
          </ActionButton>
        </div>
      ) : (
        <>
          <Field label={t("主题", "Subject")}>
            {(id) => (
              <input
                id={id}
                className={inputClass}
                disabled={locked || from?.template_only}
                maxLength={998}
                value={payload.subject}
                onChange={(e) => change({ subject: e.target.value })}
              />
            )}
          </Field>
          <Field label={t("正文", "Message")}>
            {(id) => (
              <textarea
                id={id}
                className={inputClass}
                disabled={locked || from?.template_only}
                rows={10}
                value={payload.text_body}
                onChange={(e) => change({ text_body: e.target.value })}
              />
            )}
          </Field>
          <details>
            <summary className="cursor-pointer text-sm">
              {t("可选 HTML 正文", "Optional HTML body")}
            </summary>
            <textarea
              aria-label="HTML"
              className={`${inputClass} mt-2 font-mono`}
              disabled={locked || from?.template_only}
              rows={5}
              value={payload.html_body ?? ""}
              onChange={(e) => change({ html_body: e.target.value })}
            />
          </details>
        </>
      )}
      {preview && (
        <div className="rounded-lg border p-4 space-y-3">
          <h3 className="font-medium">{preview.subject}</h3>
          <pre className="whitespace-pre-wrap text-sm">{preview.text_body}</pre>
          {preview.html_body && <MailHTML html={preview.html_body} />}
        </div>
      )}
      <Field
        label={t(
          "附件（最多 10 个、合计 20 MiB）",
          "Attachments (10 files, 20 MiB total)",
        )}
      >
        {(id) => (
          <input
            id={id}
            type="file"
            className={inputClass}
            disabled={locked || (payload.attachment_ids?.length ?? 0) >= 10}
            onChange={(e) => {
              const f = e.target.files?.[0];
              e.target.value = "";
              if (!f) return;
              void run(async () => {
                if (f.size > 20 * 1024 * 1024)
                  throw new Error(t("附件过大", "Attachment too large"));
                const fd = new FormData();
                fd.append("file", f);
                const a = await company<MailAttachment>(
                  `${workPath(mailboxId)}/attachments`,
                  { method: "POST", body: fd },
                );
                setNames((v) => ({ ...v, [a.id]: a.filename }));
                change({
                  attachment_ids: [...(payload.attachment_ids ?? []), a.id],
                });
              });
            }}
          />
        )}
      </Field>
      {(payload.attachment_ids ?? []).map((id, i) => (
        <div
          key={id}
          className="flex items-center justify-between rounded border p-2 text-sm"
        >
          <span>{names[id] ?? `${t("附件", "Attachment")} ${i + 1}`}</span>
          <div className="flex gap-2">
            <ActionButton
              onClick={() =>
                run(() =>
                  downloadCompanyFile(
                    `/attachments/${id}`,
                    names[id] ?? "attachment",
                  ),
                )
              }
              disabled={busy}
            >
              {t("下载", "Download")}
            </ActionButton>
            <ActionButton
              disabled={locked}
              onClick={() =>
                change({
                  attachment_ids: payload.attachment_ids?.filter(
                    (v) => v !== id,
                  ),
                })
              }
            >
              {t("移除", "Remove")}
            </ActionButton>
          </div>
        </div>
      ))}
      <div className="flex flex-wrap gap-3">
        <ActionButton
          disabled={busy || Boolean(pending) || !from?.can_send}
          onClick={() =>
            run(async () => {
              await save();
            })
          }
        >
          {t("保存草稿", "Save draft")}
        </ActionButton>
        <ActionButton
          className="bg-primary text-primary-foreground"
          disabled={
            busy ||
            !from?.can_send ||
            (!pending &&
              (!payload.to.length ||
                invalidVersion ||
                (from.template_only && !version)))
          }
          onClick={() => run(send)}
        >
          {busy
            ? t("处理中…", "Working…")
            : pending
              ? t("重试同一提交", "Retry same submission")
              : t("发送", "Send")}
        </ActionButton>
        <span className="self-center text-xs text-muted-foreground">
          {draft.id
            ? `${t("草稿版本", "Draft revision")} ${draft.revision}`
            : t("尚未保存", "Not saved")}
        </span>
      </div>
    </Section>
  );
}

function RecipientInput({
  id,
  disabled,
  value,
  onChange,
}: {
  id: string;
  disabled: boolean;
  value: string[];
  onChange: (v: string[]) => void;
}) {
  const [raw, setRaw] = useState(value.join(", "));
  return (
    <textarea
      id={id}
      className={inputClass}
      disabled={disabled}
      value={raw}
      onChange={(e) => {
        setRaw(e.target.value);
        onChange(addresses(e.target.value));
      }}
      placeholder="alice@example.com, bob@example.com"
      rows={2}
    />
  );
}
