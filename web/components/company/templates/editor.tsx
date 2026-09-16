"use client";
import { useState } from "react";
import { toast } from "sonner";
import {
  company,
  type MailTemplate,
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
  edit: MailTemplate | null;
  setEdit: (update: (prev: MailTemplate | null) => MailTemplate | null) => void;
  mailboxes: WorkMailbox[];
  mailbox: string;
  setMailbox: (id: string) => void;
  onSaved: (template: MailTemplate) => Promise<void>;
  onPublished: (template: MailTemplate) => Promise<void>;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const [vars, setVars] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<RenderedTemplate | null>(null);

  function update(p: Partial<TemplateDraft>) {
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
  async function save() {
    if (!edit) throw new Error("No template selected");
    const res = await company<MailTemplate>(
      edit.id ? `/templates/${edit.id}` : "/templates",
      {
        method: edit.id ? "PUT" : "POST",
        body: {
          ...edit,
          draft: {
            ...edit.draft,
            variables: edit.draft.variables.map((v) => ({
              ...v,
              options: v.options?.filter(Boolean),
            })),
          },
        },
      },
    );
    setEdit(() => res);
    await onSaved(res);
    return res;
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
            onChange={(e) => setEdit(() => ({ ...edit, name: e.target.value }))}
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
          disabled={busy || !edit.name}
          onClick={() =>
            run(async () => {
              await save();
              toast.success(t("草稿已保存", "Draft saved"));
            })
          }
        >
          {t("保存模板草稿", "Save template draft")}
        </ActionButton>
        <ActionButton
          disabled={busy || !edit.name || edit.retired}
          onClick={() =>
            run(async () => {
              const value = await save();
              await onPublished(value);
              setVars({});
              setPreview(null);
              toast.success(
                t("不可变版本已发布", "Immutable version published"),
              );
            })
          }
        >
          {t("保存并发布新版本", "Save and publish new version")}
        </ActionButton>
      </div>
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
