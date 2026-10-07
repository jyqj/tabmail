"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { sessionScope, useSessionScope } from "@/lib/session";
import { useAPI } from "@/hooks/use-api";
import {
  company,
  errorText,
  workMailboxes,
  type MailTemplate,
  type MailTemplateEditor,
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
  const [selection, setSelection] = useState<{ generation: number; edit: MailTemplateEditor | null }>({ generation: 0, edit: null });
  const edit = selection.edit;
  // Explicit selection (including selecting the same/new template again) owns
  // a separate editor. Late setters from older children cannot replace it.
  function replaceEdit(value: MailTemplateEditor) {
    setSelection(current => ({ generation: current.generation + 1, edit: value }));
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
  const currentLibrary = useRef<{ data: MailTemplate[] | undefined; ready: boolean } | null>(null);
  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  useLayoutEffect(() => {
    currentLibrary.current = { data: templates.data, ready: libraryReady };
    return () => { currentLibrary.current = null; };
  }, [templates.data, libraryReady]);
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
  async function onSaved() {
    await templates.mutate();
  }
  // After an emergency version revoke the template revision has moved, so the
  // selected copy must be re-synced before any further retire/publish CAS.
  async function onVersionRevoked() {
    const list = await templates.mutate();
    if (edit) {
      const fresh = (list ?? []).find((tpl) => tpl.id === edit.id);
      if (fresh) setEdit(() => fresh);
    }
    setVersionKey((k) => k + 1);
  }
  async function onPublished(value: MailTemplate) {
    const list = await templates.mutate();
    const fresh = list?.find(template => template.id === value.id);
    if (!fresh) throw new Error(t("发布后的模板暂时无法读取，请重试刷新。", "The published template is unavailable. Retry the refresh."));
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
          />
        </TabsContent>
        <TabsContent value="versions">
          <TemplateVersionsView
            template={edit?.id ? edit : null}
            refreshKey={versionKey}
            onRevoked={onVersionRevoked}
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
