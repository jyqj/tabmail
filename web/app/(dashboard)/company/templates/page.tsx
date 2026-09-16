"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import {
  allEmployees,
  company,
  workMailboxes,
  type MailTemplate,
  type TemplateDraft,
  type TemplateVariable,
  type TemplateVersion,
  type RenderedTemplate,
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
import { EmployeeField } from "@/components/company/employee-field";

export default function TemplatesPage() {
  const t = useText();
  const { busy, run } = useAction();
  const templates = useAPI("company-templates", () =>
    company<MailTemplate[]>("/templates"),
  );
  const boxes = useAPI("template-mailboxes", workMailboxes);
  const users = useAPI("template-users", allEmployees);
  const [edit, setEdit] = useState<MailTemplate | null>(null);
  const [mailbox, setMailbox] = useState("");
  const [grantee, setGrantee] = useState("");
  const [vars, setVars] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<RenderedTemplate | null>(null);
  const versions = useAPI(
    edit?.id ? ["template-versions", edit.id] : null,
    () => company<TemplateVersion[]>(`/templates/${edit!.id}/versions`),
  );
  const grants = useAPI(edit?.id ? ["template-grants", edit.id] : null, () =>
    company<{ user_id: string; mailbox_id: string }[]>(
      `/templates/${edit!.id}/grants`,
    ),
  );
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
    setEdit(res);
    await templates.mutate();
    return res;
  }
  const activeMailbox = mailbox || boxes.data?.[0]?.mailbox.id || "";
  return (
    <main className="mx-auto max-w-6xl w-full p-4 md:p-7 space-y-6">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">
            {t("管理员发送模板", "Administrator mail templates")}
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {t(
              "草稿可编辑；发布版本不可修改。模板授权不会授予额外的发件身份。",
              "Drafts are editable; published versions are immutable. Template grants do not grant additional sender identities.",
            )}
          </p>
        </div>
        <ActionButton
          disabled={busy}
          onClick={() => {
            setEdit({
              name: "",
              revision: 0,
              draft: {
                subject: t("致 {{.customer_name}}", "Hello {{.customer_name}}"),
                text_body: t(
                  "您好 {{.customer_name}}，\n\n{{.employee_name}}\n{{.company_name}}",
                  "Hello {{.customer_name}},\n\n{{.employee_name}}\n{{.company_name}}",
                ),
                html_body: "",
                variables: [
                  {
                    name: "customer_name",
                    type: "text",
                    required: true,
                    max_length: 100,
                  },
                ],
              },
            });
            setVars({});
            setPreview(null);
          }}
        >
          {t("新建模板", "New template")}
        </ActionButton>
      </header>
      <LoadError
        error={templates.error || boxes.error || users.error}
        onRetry={() => {
          void templates.mutate();
          void boxes.mutate();
          void users.mutate();
        }}
      />
      <Section title={t("模板库", "Template library")}>
        {(templates.data ?? []).map((v) => (
          <div
            key={v.id}
            className="flex flex-wrap justify-between gap-3 border-b pb-3"
          >
            <button
              className="text-left"
              onClick={() => {
                setEdit(structuredClone(v));
                setVars({});
                setPreview(null);
              }}
            >
              <p className="font-medium">{v.name}</p>
              <p className="text-xs text-muted-foreground">
                {t("草稿修订", "Draft revision")} {v.revision} ·{" "}
                {v.retired ? t("已停用", "Retired") : t("可用", "Active")}
              </p>
            </button>
            <ActionButton
              disabled={busy}
              onClick={() =>
                run(async () => {
                  await company(`/templates/${v.id}/retire`, {
                    method: "POST",
                    body: { revision: v.revision, retired: !v.retired },
                  });
                  await templates.mutate();
                  if (edit?.id === v.id) setEdit(null);
                })
              }
            >
              {v.retired
                ? t("重新启用", "Reactivate")
                : t("停用（包括待发任务）", "Retire (including queued sends)")}
            </ActionButton>
          </div>
        ))}
        {!templates.isLoading && !templates.data?.length && (
          <p>{t("尚无模板", "No templates yet")}</p>
        )}
      </Section>
      {edit && (
        <Section title={t("编辑与发布", "Edit and publish")}>
          <Field label={t("模板名称", "Template name")}>
            {(id) => (
              <input
                id={id}
                className={inputClass}
                value={edit.name}
                maxLength={120}
                onChange={(e) => setEdit({ ...edit, name: e.target.value })}
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
                  await company(`/templates/${value.id}/publish`, {
                    method: "POST",
                    body: { revision: value.revision },
                  });
                  await templates.mutate();
                  await versions.mutate();
                  setEdit(null);
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
                value={activeMailbox}
                onChange={(e) => {
                  setMailbox(e.target.value);
                  setPreview(null);
                }}
              >
                <option value="" />
                {(boxes.data ?? []).map((v) => (
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
            disabled={busy || !activeMailbox}
            onClick={() =>
              run(async () =>
                setPreview(
                  await company<RenderedTemplate>("/templates/preview", {
                    method: "POST",
                    body: {
                      mailbox_id: activeMailbox,
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
          {edit.id && (
            <>
              <h3 className="font-semibold border-t pt-4">
                {t(
                  "已发布版本与使用授权",
                  "Published versions and usage grants",
                )}
              </h3>
              <LoadError
                error={versions.error || grants.error}
                onRetry={() => {
                  void versions.mutate();
                  void grants.mutate();
                }}
              />
              {(versions.data ?? []).map((v) => (
                <details key={v.id} className="text-sm">
                  <summary>
                    v{v.version} · {new Date(v.published_at).toLocaleString()} ·{" "}
                    {v.content_hash.slice(0, 16)}
                  </summary>
                  <pre className="whitespace-pre-wrap p-3">
                    {v.snapshot.subject}
                    {"\n"}
                    {v.snapshot.text_body}
                  </pre>
                </details>
              ))}
              <EmployeeField
                label={t(
                  "允许使用模板的成员",
                  "Member allowed to use the template",
                )}
                value={grantee}
                onChange={setGrantee}
                employees={(users.data ?? []).filter((u) => u.is_active)}
              />
              <ActionButton
                disabled={busy || !grantee || !activeMailbox}
                onClick={() =>
                  run(async () => {
                    await company(`/templates/${edit.id}/grants`, {
                      method: "PUT",
                      body: {
                        user_id: grantee,
                        mailbox_id: activeMailbox,
                        enabled: true,
                      },
                    });
                    await grants.mutate();
                    toast.success(
                      t("模板使用授权已添加", "Template usage granted"),
                    );
                  })
                }
              >
                {t(
                  "授权此成员在所选邮箱使用",
                  "Grant usage on the selected mailbox",
                )}
              </ActionButton>
              {(grants.data ?? []).map((g) => (
                <div
                  key={`${g.user_id}:${g.mailbox_id}`}
                  className="flex flex-wrap items-center justify-between gap-3 text-sm"
                >
                  <span>
                    {users.data?.find((u) => u.id === g.user_id)?.email ??
                      g.user_id}{" "}
                    ·{" "}
                    {boxes.data?.find((b) => b.mailbox.id === g.mailbox_id)
                      ?.mailbox.full_address ?? g.mailbox_id}
                  </span>
                  <ActionButton
                    disabled={busy}
                    onClick={() =>
                      run(async () => {
                        await company(`/templates/${edit.id}/grants`, {
                          method: "PUT",
                          body: { ...g, enabled: false },
                        });
                        await grants.mutate();
                      })
                    }
                  >
                    {t("撤销使用权", "Revoke usage")}
                  </ActionButton>
                </div>
              ))}
            </>
          )}
        </Section>
      )}
    </main>
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
