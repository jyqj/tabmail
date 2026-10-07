"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { sessionScope, useSessionScope } from "@/lib/session";
import {
  company,
  type MailTemplate,
  type MailTemplateEditor,
  type RenderedTemplate,
  type TemplateDraft,
  type TemplateVariable,
  type WorkMailbox,
} from "@/lib/company";
import {
  ActionButton,
  Field,
  inputClass,
  MailHTML,
  Section,
  useAction,
  useText,
} from "@/components/company/common";

function sameFields(left: Pick<MailTemplate, "name" | "draft">, right: Pick<MailTemplate, "name" | "draft">) {
  return left.name === right.name && JSON.stringify(left.draft) === JSON.stringify(right.draft);
}

// Only the response metadata is authoritative for edits made after the saved
// snapshot. Merge unchanged fields so server normalization is still applied.
function reconcileSaved(current: MailTemplateEditor | null, snapshot: MailTemplateEditor, saved: MailTemplate): MailTemplateEditor | null {
  if (!current || current.id !== snapshot.id || current.revision !== snapshot.revision) return current;
  return {
    ...saved,
    name: current.name === snapshot.name ? saved.name : current.name,
    draft: {
      subject: current.draft.subject === snapshot.draft.subject ? saved.draft.subject : current.draft.subject,
      text_body: current.draft.text_body === snapshot.draft.text_body ? saved.draft.text_body : current.draft.text_body,
      html_body: current.draft.html_body === snapshot.draft.html_body ? saved.draft.html_body : current.draft.html_body,
      variables: JSON.stringify(current.draft.variables) === JSON.stringify(snapshot.draft.variables)
        ? saved.draft.variables : current.draft.variables,
    },
  };
}

type PublishedSnapshot = { saved: MailTemplate; owner: object; edits: number };

// TemplateEditorView edits the mutable draft: name, subject, text/HTML body
// and the declared variables. It also owns server-side validation/preview and
// the save / save-and-publish actions that create immutable versions.
export function TemplateEditorView({
  edit,
  setEdit,
  mailboxes,
  mailbox,
  setMailbox,
  onSaved,
  onPublished,
}: {
  edit: MailTemplateEditor | null;
  setEdit: (update: (prev: MailTemplateEditor | null) => MailTemplateEditor | null) => void;
  mailboxes: WorkMailbox[];
  mailbox: string;
  setMailbox: (id: string) => void;
  onSaved: (template: MailTemplate) => Promise<void>;
  /** Refresh only: publication is already acknowledged before this callback. */
  onPublished: (template: MailTemplate) => Promise<MailTemplate>;
}) {
  const t = useText();
  const scope = useSessionScope();
  const [editorScope] = useState(sessionScope);
  const { busy, run } = useAction();
  const [vars, setVars] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<RenderedTemplate | null>(null);
  const [publication, setPublication] = useState<PublishedSnapshot | null>(null);
  const lifetime = useRef<object | null>(null);
  const committed = useRef(edit);
  const edits = useRef(0);
  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  useLayoutEffect(() => { committed.current = edit; }, [edit]);
  const owns = (owner: object) => lifetime.current === owner && editorScope === sessionScope();

  function update(p: Partial<TemplateDraft>) {
    edits.current += 1;
    setEdit((v) => (v ? { ...v, draft: { ...v.draft, ...p } } : v));
    setPreview(null);
  }
  function updateVariable(index: number, p: Partial<TemplateVariable>) {
    update({
      variables: edit!.draft.variables.map((v, i) =>
        i === index ? { ...v, ...p } : v,
      ),
    });
  }
  async function finishPublication(published: PublishedSnapshot) {
    if (!owns(published.owner)) return;
    try {
      const fresh = await onPublished(published.saved);
      if (!owns(published.owner)) return;
      // Publish increments the template revision but returns a version, not
      // that new revision. Reconcile only a fresh authoritative template read.
      if (fresh.id !== published.saved.id || !Number.isSafeInteger(fresh.revision) || fresh.revision <= published.saved.revision)
        throw new Error(t("发布已完成，但尚未读取到新的模板版本。请重试刷新。", "Publication completed, but the new template revision is unavailable. Retry the refresh."));
      const changed = edits.current !== published.edits;
      if (changed && !sameFields(fresh, published.saved))
        throw new Error(t("发布后模板内容又发生了变化。请保留当前编辑，并从模板库核对最新内容。", "The template changed again after publication. Keep these edits and review the latest content from the library."));
      setEdit(current => {
        if (changed) return reconcileSaved(current, published.saved, fresh);
        return current?.id === published.saved.id && current.revision === published.saved.revision ? null : current;
      });
      setPublication(null);
      setVars({});
      setPreview(null);
      toast.success(changed
        ? t("不可变版本已发布；后续编辑尚未保存。", "Immutable version published; newer edits remain unsaved.")
        : t("不可变版本已发布", "Immutable version published"));
    } catch (error) {
      if (owns(published.owner)) throw error;
    }
  }
  async function save(publish: boolean) {
    const owner = lifetime.current;
    if (!edit || !owner || !owns(owner) || publication) return;
    const snapshot = edit;
    const started = edits.current;
    try {
      const res = await company<MailTemplate>(
        snapshot.id ? `/templates/${snapshot.id}` : "/templates",
        {
          method: snapshot.id ? "PUT" : "POST",
          body: {
            name: snapshot.name,
            revision: snapshot.revision,
            draft: {
              ...snapshot.draft,
              variables: snapshot.draft.variables.map((v) => ({
                ...v,
                options: v.options?.filter(Boolean),
              })),
            },
          },
        },
      );
      if (!owns(owner)) return;
      if (!res.id || (snapshot.id && res.id !== snapshot.id) || !Number.isSafeInteger(res.revision) || res.revision <= snapshot.revision)
        throw new Error(t("保存结果缺少有效的模板版本，请核对模板。", "The save result has no valid template revision. Review the template."));
      if (!committed.current || committed.current.id !== snapshot.id || committed.current.revision !== snapshot.revision) return;
      setEdit(current => reconcileSaved(current, snapshot, res));
      await onSaved(res);
      if (!owns(owner)) return;
      if (!publish || edits.current !== started) {
        toast.success(edits.current !== started
          ? t("请求中的草稿已保存；后续编辑尚未保存，请核对后再保存或发布。", "Draft saved; newer edits remain unsaved. Review them before saving or publishing.")
          : t("草稿已保存", "Draft saved"));
        return;
      }
      await company(`/templates/${res.id}/publish`, { method: "POST", body: { revision: res.revision } });
      if (!owns(owner)) return;
      const published = { owner, saved: res, edits: started };
      setPublication(published);
      await finishPublication(published);
    } catch (error) {
      if (owns(owner)) throw error;
    }
  }
  if (!edit) {
    return (
      <Section title={t("编辑与发布", "Edit and publish")}>
        <p className="text-muted-foreground">
          {t(
            "从模板库选择一个模板，或点击右上角“新建模板”。",
            "Pick a template from the library, or use “New template” above.",
          )}
        </p>
      </Section>
    );
  }
  return (
    <Section title={t("编辑与发布", "Edit and publish")}>
      <Field label={t("模板名称", "Template name")}>
        {(id) => (
          <input
            id={id}
            className={inputClass}
            value={edit.name}
            maxLength={120}
            onChange={(e) => {
              edits.current += 1;
              const name = e.target.value;
              setEdit(value => value ? { ...value, name } : value);
            }}
          />
        )}
      </Field>
      <Field label={t("主题模板", "Subject template")}>
        {(id) => (
          <input
            id={id}
            className={inputClass}
            value={edit.draft.subject}
            maxLength={998}
            onChange={(e) => update({ subject: e.target.value })}
          />
        )}
      </Field>
      <Field label={t("纯文本正文模板", "Text body template")}>
        {(id) => (
          <textarea
            id={id}
            className={inputClass}
            rows={8}
            value={edit.draft.text_body}
            onChange={(e) => update({ text_body: e.target.value })}
          />
        )}
      </Field>
      <details>
        <summary>{t("可选 HTML 模板", "Optional HTML template")}</summary>
        <textarea
          aria-label="HTML template"
          className={`${inputClass} font-mono mt-2`}
          rows={6}
          value={edit.draft.html_body}
          onChange={(e) => update({ html_body: e.target.value })}
        />
      </details>
      <p className="rounded bg-muted p-3 text-sm">
        {t(
          "仅支持 {{.变量名}}。系统变量 employee_name、company_name、sender_address 由服务器填写，不能由员工覆盖。",
          "Only {{.variable_name}} placeholders are supported. employee_name, company_name and sender_address are server-controlled.",
        )}
      </p>
      {edit.draft.variables.map((v, i) => (
        <div
          className="rounded border p-3 grid gap-3 md:grid-cols-3"
          key={i}
        >
          <Field label={t("变量名", "Variable name")}>
            {(id) => (
              <input
                id={id}
                className={inputClass}
                value={v.name}
                onChange={(e) =>
                  updateVariable(i, { name: e.target.value })
                }
              />
            )}
          </Field>
          <Field label={t("类型", "Type")}>
            {(id) => (
              <select
                id={id}
                className={inputClass}
                value={v.type}
                onChange={(e) =>
                  updateVariable(i, {
                    type: e.target.value as TemplateVariable["type"],
                  })
                }
              >
                {["text", "email", "integer", "date", "url"].map((x) => (
                  <option key={x}>{x}</option>
                ))}
              </select>
            )}
          </Field>
          <Field label={t("最大长度", "Maximum length")}>
            {(id) => (
              <input
                id={id}
                type="number"
                className={inputClass}
                min={1}
                max={4000}
                value={v.max_length}
                onChange={(e) =>
                  updateVariable(i, { max_length: Number(e.target.value) })
                }
              />
            )}
          </Field>
          <label className="flex gap-2 items-center text-sm">
            <input
              type="checkbox"
              checked={v.required}
              onChange={(e) =>
                updateVariable(i, { required: e.target.checked })
              }
            />
            {t("必填", "Required")}
          </label>
          <OptionsField
            value={v.options ?? []}
            onChange={(options) => updateVariable(i, { options })}
          />
          <ActionButton
            onClick={() =>
              update({
                variables: edit.draft.variables.filter(
                  (_, index) => index !== i,
                ),
              })
            }
          >
            {t("移除变量", "Remove variable")}
          </ActionButton>
        </div>
      ))}
      <ActionButton
        disabled={edit.draft.variables.length >= 32}
        onClick={() =>
          update({
            variables: [
              ...edit.draft.variables,
              {
                name: `field_${edit.draft.variables.length + 1}`,
                type: "text",
                required: true,
                max_length: 200,
              },
            ],
          })
        }
      >
        {t("添加变量", "Add variable")}
      </ActionButton>
      <div className="flex flex-wrap gap-3">
        <ActionButton
          disabled={busy || !!publication || editorScope !== scope || !edit.name}
          onClick={() => run(() => save(false))}
        >
          {t("保存模板草稿", "Save template draft")}
        </ActionButton>
        <ActionButton
          disabled={busy || !!publication || editorScope !== scope || !edit.name || edit.retired}
          onClick={() => run(() => save(true))}
        >
          {t("保存并发布新版本", "Save and publish new version")}
        </ActionButton>
      </div>
      {publication && <div role="status" className="space-y-2 rounded border p-3 text-sm">
        <p>{t("版本已发布。继续保存或发布前，请先读取最新模板版本；当前编辑已保留。", "The version was published. Refresh the template revision before saving or publishing again; current edits are retained.")}</p>
        <ActionButton disabled={busy || editorScope !== scope} onClick={() => run(() => finishPublication(publication))}>
          {t("刷新模板版本", "Refresh template revision")}
        </ActionButton>
      </div>}
      <Field label={t("预览 / 授权所用邮箱", "Preview / grant mailbox")}>
        {(id) => (
          <select
            id={id}
            className={inputClass}
            value={mailbox}
            onChange={(e) => {
              setMailbox(e.target.value);
              setPreview(null);
            }}
          >
            <option value="" />
            {mailboxes.map((v) => (
              <option key={v.mailbox.id} value={v.mailbox.id}>
                {v.mailbox.full_address}
              </option>
            ))}
          </select>
        )}
      </Field>
      {edit.draft.variables.map((v, i) => (
        <Field key={i} label={`${t("预览值", "Preview value")}: ${v.name}`}>
          {(id) => (
            <input
              id={id}
              className={inputClass}
              maxLength={v.max_length}
              value={vars[v.name] ?? ""}
              onChange={(e) => {
                setVars({ ...vars, [v.name]: e.target.value });
                setPreview(null);
              }}
            />
          )}
        </Field>
      ))}
      <ActionButton
        disabled={busy || !mailbox}
        onClick={() =>
          run(async () =>
            setPreview(
              await company<RenderedTemplate>("/templates/preview", {
                method: "POST",
                body: {
                  mailbox_id: mailbox,
                  draft: edit.draft,
                  vars,
                },
              }),
            ),
          )
        }
      >
        {t("服务端校验与预览", "Validate and preview on server")}
      </ActionButton>
      {preview && (
        <div className="rounded border p-4">
          <h3 className="font-semibold">{preview.subject}</h3>
          <pre className="whitespace-pre-wrap text-sm my-4">
            {preview.text_body}
          </pre>
          {preview.html_body && <MailHTML html={preview.html_body} />}
        </div>
      )}
    </Section>
  );
}
function OptionsField({
  value,
  onChange,
}: {
  value: string[];
  onChange: (v: string[]) => void;
}) {
  const t = useText();
  return (
    <Field
      label={t(
        "允许值（可选，每行一个）",
        "Allowed values (optional, one per line)",
      )}
    >
      {(id) => (
        <textarea
          id={id}
          className={inputClass}
          rows={2}
          value={value.join("\n")}
          onChange={(e) => onChange(e.target.value.split("\n"))}
        />
      )}
    </Field>
  );
}
