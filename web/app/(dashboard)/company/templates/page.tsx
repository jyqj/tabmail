"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { sessionScope, useSessionScope } from "@/lib/session";
import { useAPI } from "@/hooks/use-api";
import {
  company,
  errorText,
  revokeTemplateVersion,
  workMailboxes,
  type MailTemplate,
  type MailTemplateEditor,
  type TemplateDraft,
  type TemplateVersion,
} from "@/lib/company";
import {
  ActionButton,
  LoadError,
  useAction,
  useText,
} from "@/components/company/common";
import { TemplateLibraryView } from "@/components/company/templates/library";
import { TemplateEditorView } from "@/components/company/templates/editor";
import { TemplateVersionsView } from "@/components/company/templates/versions";
import { TemplateGrantsView } from "@/components/company/templates/grants";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";

type TemplateTab = "library" | "editor" | "versions" | "grants";
type VersionReview = {
  owner: object;
  id: string;
  revision: number;
  baseline: MailTemplate;
  acknowledged: boolean;
  fresh?: MailTemplate;
  error?: Error;
};
type TemplateSelection = {
  generation: number;
  edit: MailTemplateEditor | null;
  baseline: MailTemplateEditor | null;
  review: VersionReview | null;
};

// A version revocation changes metadata independently of draft editing. Merge
// only fields whose original observation still agrees; conflicts stay visible
// until the administrator explicitly chooses the preserved local edits.
function mergeVersionDraft(current: MailTemplateEditor, baseline: MailTemplate, fresh: MailTemplate, reviewed: boolean) {
  let conflict = false;
  function field<T>(local: T, before: T, remote: T): T {
    const same = (a: T, b: T) => JSON.stringify(a) === JSON.stringify(b);
    if (same(local, before)) return remote;
    if (!same(remote, before) && !same(local, remote) && !reviewed) conflict = true;
    return local;
  }
  const draft: TemplateDraft = {
    subject: field(current.draft.subject, baseline.draft.subject, fresh.draft.subject),
    text_body: field(current.draft.text_body, baseline.draft.text_body, fresh.draft.text_body),
    html_body: field(current.draft.html_body, baseline.draft.html_body, fresh.draft.html_body),
    variables: field(current.draft.variables, baseline.draft.variables, fresh.draft.variables),
  };
  const edit: MailTemplate = { ...fresh, name: field(current.name, baseline.name, fresh.name), draft };
  return { edit, conflict };
}

// The administrator template surface is split into four views sharing one
// selected template: the library (list + retire), the draft editor (with
// server-side preview), the immutable version history and the per-mailbox
// usage grants. API calls are unchanged; only the presentation is tabbed.
export default function TemplatesPage() {
  const scope = useSessionScope();
  return <TemplatesSession key={scope} scope={scope} />;
}

function TemplatesSession({ scope }: { scope: string }) {
  const t = useText();
  const { busy, run } = useAction();
  async function readTemplateLibrary() {
    const list = await company<MailTemplate[]>("/templates");
    if (!Array.isArray(list)) {
      throw new Error(t("模板库响应无效，请重新加载。", "Invalid template library response. Reload the library."));
    }
    return list;
  }
  const templates = useAPI("company-templates", readTemplateLibrary);
  const boxes = useAPI("template-mailboxes", workMailboxes);
  const [tab, setTab] = useState<TemplateTab>("library");
  const [selection, setSelection] = useState<TemplateSelection>({ generation: 0, edit: null, baseline: null, review: null });
  const edit = selection.edit;
  // Explicit selection (including selecting the same/new template again) owns
  // a separate editor. Late setters from older children cannot replace it.
  function replaceEdit(value: MailTemplateEditor) {
    setSelection(current => ({ generation: current.generation + 1, edit: value, baseline: value, review: null }));
  }
  function setEdit(update: (value: MailTemplateEditor | null) => MailTemplateEditor | null) {
    setSelection(current => current.generation === selection.generation
      ? { ...current, edit: update(current.edit) } : current);
  }
  const [mailbox, setMailbox] = useState("");
  const [versionKey, setVersionKey] = useState(0);
  const activeMailbox = mailbox || boxes.data?.[0]?.mailbox.id || "";
  const [checkingStatus, setCheckingStatus] = useState(false);
  const [retireRecovery, setRetireRecovery] = useState<Error | null>(null);
  const libraryError = retireRecovery || templates.error;
  const libraryValidating = checkingStatus || templates.isValidating;
  const libraryReady = !libraryError && !templates.isLoading &&
    !libraryValidating && Array.isArray(templates.data);
  const lifetime = useRef<object | null>(null);
  const currentSelection = useRef(selection);
  const currentLibrary = useRef<{ data: MailTemplate[] | undefined; ready: boolean } | null>(null);
  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  useLayoutEffect(() => {
    currentLibrary.current = { data: templates.data, ready: libraryReady };
    return () => { currentLibrary.current = null; };
  }, [templates.data, libraryReady]);
  useLayoutEffect(() => { currentSelection.current = selection; }, [selection]);
  const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();

  async function refreshTemplateLibrary() {
    const owner = lifetime.current;
    if (!owner || !owns(owner)) return;
    setCheckingStatus(true);
    try {
      // A revalidation can resolve with retained cached data after its GET
      // fails. Use an explicit read promise so recovery confirms a new result.
      const fresh = await templates.mutate(async () => {
        const list = await readTemplateLibrary();
        if (!owns(owner)) throw new DOMException("Template view changed", "AbortError");
        return list;
      }, { revalidate: false });
      if (owns(owner)) setRetireRecovery(null);
      return fresh;
    } catch (error) {
      // A failed read can reject before the success-path ownership check.
      // Keep old retries from reporting errors in a different template view.
      if (!owns(owner)) throw new DOMException("Template view changed", "AbortError");
      throw error;
    } finally {
      if (owns(owner)) setCheckingStatus(false);
    }
  }

  function selectTemplate(template: MailTemplate) {
    replaceEdit(structuredClone(template));
    setTab("editor");
  }
  function newTemplate() {
    replaceEdit({
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
    setMailbox("");
    setTab("editor");
  }
  function retireTemplate(template: MailTemplate) {
    const owner = lifetime.current;
    const observed = currentLibrary.current;
    if (!owner || !owns(owner) || !observed?.ready ||
      !observed.data?.includes(template)) return Promise.resolve();
    return run(async () => {
      let acknowledged = false;
      try {
        await company(`/templates/${template.id}/retire`, {
          method: "POST",
          body: { revision: template.revision, retired: !template.retired },
        });
        acknowledged = true;
        if (!owns(owner)) return;
        await refreshTemplateLibrary();
        if (owns(owner) && edit?.id === template.id) {
          setEdit(current => current === edit ? null : current);
        }
      } catch (error) {
        if (!owns(owner)) return;
        const message = acknowledged
          ? t("模板状态已保存，但模板库刷新失败。请重试加载以核对当前状态。", "Template status was saved, but the library could not be refreshed. Retry loading to check the current state.")
          : t("无法确认模板状态是否已变更。请先重新加载模板库，再决定是否重试。", "The template status change could not be confirmed. Reload the library before trying again.");
        const recovery = new Error(`${message} ${errorText(error)}`);
        setRetireRecovery(recovery);
        throw recovery;
      }
    });
  }
  async function onSaved(saved: MailTemplate) {
    setSelection(current => current.generation === selection.generation && current.edit?.id === saved.id &&
      current.edit.revision === saved.revision ? { ...current, baseline: saved } : current);
    await templates.mutate();
  }

  function ownsVersionReview(owner: object, generation: number, review: VersionReview) {
    return owns(owner) && currentSelection.current.generation === generation &&
      currentSelection.current.review?.owner === review.owner;
  }
  async function refreshVersionRevision(owner: object, generation: number, review: VersionReview) {
    try {
      // Revalidation can resolve to retained cache after a failed GET. The
      // revision fence is released only by an explicit successful read here.
      const list = await readTemplateLibrary();
      if (!ownsVersionReview(owner, generation, review)) return;
      const fresh = list.find(value => value.id === review.id);
      if (!fresh || !Number.isSafeInteger(fresh.revision) || fresh.revision < review.revision ||
        (review.acknowledged && fresh.revision === review.revision) || typeof fresh.name !== "string" ||
        !fresh.draft || typeof fresh.draft.subject !== "string" || typeof fresh.draft.text_body !== "string" ||
        typeof fresh.draft.html_body !== "string" || !Array.isArray(fresh.draft.variables)) {
        throw new Error(t("尚未读取到当前模板修订，请重试刷新。", "The current template revision is unavailable. Retry the refresh."));
      }
      await templates.mutate(list, { revalidate: false });
      if (!ownsVersionReview(owner, generation, review)) return;
      setSelection(current => {
        if (current.generation !== generation || current.review?.owner !== review.owner || current.edit?.id !== review.id) return current;
        if (fresh.revision < current.edit.revision) return { ...current, review: { ...review, error: new Error(t(
          "读取到的模板修订早于已经确认的草稿，请重新刷新。",
          "The template read is older than the acknowledged draft. Refresh the revision again.",
        )) } };
        const merged = mergeVersionDraft(current.edit, review.baseline, fresh, false);
        if (merged.conflict) return { ...current, review: { ...review, fresh, error: new Error(t(
          "模板中正在编辑的字段已被其他操作修改。请核对最新内容，再决定是否保留自己的修改。当前草稿完整保留。",
          "The template changed in fields you edited. Review the latest content before keeping your edits; your draft is retained.",
        )) } };
        return { ...current, edit: merged.edit, baseline: fresh, review: null };
      });
      setVersionKey(key => key + 1);
    } catch (error) {
      if (!ownsVersionReview(owner, generation, review)) return;
      const message = review.acknowledged
        ? t("版本已撤销，但当前模板修订无法确认。请刷新修订；未保存内容已保留。", "The version was revoked, but its current template revision could not be confirmed. Refresh the revision; unsaved content is retained.")
        : t("无法确认版本撤销结果。请先刷新模板修订；未保存内容已保留。", "The version revocation could not be confirmed. Refresh the template revision first; unsaved content is retained.");
      setSelection(current => current.generation === generation && current.review?.owner === review.owner
        ? { ...current, review: { ...review, fresh: undefined, error: new Error(`${message} ${errorText(error)}`) } } : current);
    }
  }
  function revokeVersion(version: TemplateVersion) {
    const owner = lifetime.current;
    const observed = currentSelection.current;
    const baseline = observed.baseline;
    if (!owner || !owns(owner) || observed.generation !== selection.generation || observed.review ||
      !observed.edit?.id || !baseline?.id || baseline.id !== observed.edit.id || baseline.revision !== observed.edit.revision ||
      version.template_id !== observed.edit.id || version.revoked_at) return Promise.resolve();
    const generation = observed.generation;
    const review: VersionReview = { owner: {}, id: observed.edit.id, revision: observed.edit.revision, baseline, acknowledged: false };
    return run(async () => {
      setSelection(current => current.generation === generation ? { ...current, review } : current);
      try {
        await revokeTemplateVersion(review.id, version.version, review.revision);
        if (!ownsVersionReview(owner, generation, review)) return;
        const acknowledged = { ...review, acknowledged: true };
        setSelection(current => current.generation === generation && current.review?.owner === review.owner
          ? { ...current, review: acknowledged } : current);
        await refreshVersionRevision(owner, generation, acknowledged);
      } catch (error) {
        if (!ownsVersionReview(owner, generation, review)) return;
        setSelection(current => current.generation === generation && current.review?.owner === review.owner
          ? { ...current, review: { ...review, error: new Error(`${t("无法确认版本撤销结果。请先刷新模板修订；未保存内容已保留。", "The version revocation could not be confirmed. Refresh the template revision first; unsaved content is retained.")} ${errorText(error)}`) } } : current);
      }
    });
  }
  function keepReviewedEdits(review: VersionReview) {
    if (!review.fresh || scope !== sessionScope()) return;
    setSelection(current => {
      if (current.generation !== selection.generation || current.review !== review || !current.edit || !review.fresh) return current;
      return { ...current, edit: mergeVersionDraft(current.edit, review.baseline, review.fresh, true).edit,
        baseline: review.fresh, review: null };
    });
  }
  async function onPublished(value: MailTemplate) {
    const list = await templates.mutate();
    const fresh = list?.find(template => template.id === value.id);
    if (!fresh) throw new Error(t("发布后的模板暂时无法读取，请重试刷新。", "The published template is unavailable. Retry the refresh."));
    setSelection(current => current.generation === selection.generation && current.edit?.id === value.id
      ? { ...current, baseline: fresh } : current);
    setVersionKey((k) => k + 1);
    return fresh;
  }
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
        <ActionButton disabled={busy} onClick={newTemplate}>
          {t("新建模板", "New template")}
        </ActionButton>
      </header>
      <LoadError
        error={libraryError || boxes.error}
        onRetry={() => {
          void run(async () => {
            await refreshTemplateLibrary();
            await boxes.mutate();
          });
        }}
      />
      {selection.review && <div role={selection.review.error ? "alert" : "status"} className="rounded border p-3 space-y-3 text-sm">
        <p>{selection.review.error?.message ?? t("正在更新模板版本，未保存的编辑会保留。", "Updating the template version; unsaved edits are retained.")}</p>
        {selection.review.fresh && <details>
          <summary>{t("核对最新模板内容", "Review the latest template content")}</summary>
          <dl className="mt-3 space-y-3">
            {[
              [t("模板名称", "Template name"), selection.review.fresh.name],
              [t("主题模板", "Subject template"), selection.review.fresh.draft.subject],
              [t("纯文本正文模板", "Text body template"), selection.review.fresh.draft.text_body],
              [t("HTML 模板", "HTML template"), selection.review.fresh.draft.html_body],
            ].map(([label, value]) => <div key={label}><dt className="font-medium">{label}</dt>
              <dd><pre className="whitespace-pre-wrap break-words">{value}</pre></dd></div>)}
            <div><dt className="font-medium">{t("变量", "Variables")}</dt><dd>
              {selection.review.fresh.draft.variables.length === 0 ? t("无变量", "No variables") : <ul className="list-disc pl-5">
                {selection.review.fresh.draft.variables.map((variable, index) => <li key={index}>
                  {variable.name} · {variable.type} · {variable.required ? t("必填", "Required") : t("可选", "Optional")} · {t("最大长度", "Maximum length")}: {variable.max_length}
                  {variable.options?.length ? ` · ${t("允许值", "Allowed values")}: ${variable.options.join(", ")}` : ""}
                </li>)}
              </ul>}
            </dd></div>
          </dl>
        </details>}
        <div className="flex flex-wrap gap-2">
          <ActionButton disabled={busy} onClick={() => {
            const owner = lifetime.current;
            const review = selection.review;
            if (owner && review && ownsVersionReview(owner, selection.generation, review))
              void run(() => refreshVersionRevision(owner, selection.generation, review));
          }}>{t("刷新模板修订", "Refresh template revision")}</ActionButton>
          {selection.review.fresh && <ActionButton disabled={busy} onClick={() => keepReviewedEdits(selection.review!)}>
            {t("采用已核对修订并保留我的编辑", "Keep my edits with the reviewed revision")}
          </ActionButton>}
        </div>
      </div>}
      <Tabs
        value={tab}
        onValueChange={(v) => setTab(v as TemplateTab)}
        className="gap-4"
      >
        <TabsList variant="line" className="flex-wrap">
          <TabsTrigger value="library">
            {t("模板库", "Template library")}
          </TabsTrigger>
          <TabsTrigger value="editor">
            {t("编辑器", "Editor")}
            {edit ? `: ${edit.name || t("未命名", "Untitled")}` : ""}
          </TabsTrigger>
          <TabsTrigger value="versions">
            {t("版本历史", "Version history")}
          </TabsTrigger>
          <TabsTrigger value="grants">
            {t("使用授权", "Usage grants")}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="library">
          <TemplateLibraryView
            templates={templates.data}
            busy={busy}
            isLoading={templates.isLoading}
            isValidating={libraryValidating}
            error={libraryError}
            selectedId={edit?.id}
            onSelect={selectTemplate}
            onRetire={retireTemplate}
          />
        </TabsContent>
        <TabsContent value="editor">
          <TemplateEditorView
            key={selection.generation}
            edit={edit}
            setEdit={setEdit}
            mailboxes={boxes.data ?? []}
            mailbox={activeMailbox}
            setMailbox={setMailbox}
            onSaved={onSaved}
            onPublished={onPublished}
            writeBlocked={!!selection.review}
          />
        </TabsContent>
        <TabsContent value="versions">
          <TemplateVersionsView
            template={edit?.id ? edit : null}
            refreshKey={versionKey}
            onRevoke={revokeVersion}
            disabled={busy || !!selection.review}
          />
        </TabsContent>
        <TabsContent value="grants">
          <TemplateGrantsView
            template={edit?.id ? edit : null}
            mailboxes={boxes.data ?? []}
            mailboxesReady={!boxes.error && !boxes.isLoading && !boxes.isValidating && Array.isArray(boxes.data)}
            mailbox={activeMailbox}
            setMailbox={setMailbox}
          />
        </TabsContent>
      </Tabs>
    </main>
  );
}
