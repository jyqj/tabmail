"use client";
import { useState } from "react";
import { useSessionScope } from "@/lib/session";
import { useAPI } from "@/hooks/use-api";
import {
  company,
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
  return <TemplatesSession key={scope} />;
}

function TemplatesSession() {
  const t = useText();
  const { busy, run } = useAction();
  const templates = useAPI("company-templates", () =>
    company<MailTemplate[]>("/templates"),
  );
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
    return run(async () => {
      await company(`/templates/${template.id}/retire`, {
        method: "POST",
        body: { revision: template.revision, retired: !template.retired },
      });
      await templates.mutate();
      if (edit?.id === template.id) setEdit(() => null);
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
        error={templates.error || boxes.error}
        onRetry={() => {
          void templates.mutate();
          void boxes.mutate();
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
            mailbox={activeMailbox}
            setMailbox={setMailbox}
          />
        </TabsContent>
      </Tabs>
    </main>
  );
}
