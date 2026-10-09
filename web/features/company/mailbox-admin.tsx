"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { ActionButton, Field, inputClass, LoadError, Section, useText } from "@/components/company/common";
import { company, allEmployees, errorText, workMailboxes } from "@/lib/company";
import { sessionScope, useSessionScope } from "@/lib/session";
import { GrantEditor } from "@/components/company/grants";
import { EmployeeField } from "@/components/company/employee-field";
import { MailboxSendPolicyEditor } from "@/components/company/send-policy";
import { AccessExplanationPanel } from "./access-explanation";
// Validate the entered decimal exactly before converting it to wire hours.
// Number alone rounds tiny fractions to zero, which would mean permanence.
function parseRetentionHours(value: string): number | null {
    const match = /^([+-]?)(\d*)(?:\.(\d*))?(?:[eE]([+-]?\d+))?$/.exec(value);
    const hours = Number(value);
    if (!match || !Number.isSafeInteger(hours) || hours < 0 || hours > 876000) return null;
    const digits = match[2] + (match[3] ?? "");
    if (!digits) return null;
    const trailingZeros = digits.length - digits.replace(/0+$/, "").length;
    const fractionalPlaces = (match[3]?.length ?? 0) - Number(match[4] ?? "0");
    if (/[1-9]/.test(digits) && fractionalPlaces > trailingZeros) return null;
    return hours;
}
export function MailboxAdmin() {
    const t = useText();
    const scope = useSessionScope();
    // Preserve unfinished form input across sessions while retiring only the
    // old request's ability to clear it, refresh the view or own its busy state.
    const [view, setView] = useState({ scope });
    if (view.scope !== scope) setView({ scope });
    const currentView = useRef<object | null>(null);
    const activeCreate = useRef<{ view: object } | null>(null);
    const currentReadback = useRef<object | null>(null);
    const [pendingView, setPendingView] = useState<object | null>(null);
    const [creationError, setCreationError] = useState<{ view: object; error: Error } | null>(null);
    const busy = pendingView === view;
    const readbackError = creationError?.view === view ? creationError.error : null;
    const draftIntent = useRef<object>({});
    const currentDraft = useRef("");
    useLayoutEffect(() => {
        currentView.current = view;
        return () => { currentView.current = null; };
    }, [view]);
    const owns = (owner: object) => currentView.current === owner && scope === sessionScope();
    const members = useAPI("company-employees", allEmployees);
    const boxes = useAPI("company-managed-mailboxes", workMailboxes);
    const [boxLocal, setBoxLocal] = useState("");
    const [kind, setKind] = useState("shared");
    const [ownerSelection, setOwnerSelection] = useState({ scope, id: "" });
    const [retention, setRetention] = useState("0");
    // An empty field is unfinished input, not an instruction for permanence.
    const retentionHours = parseRetentionHours(retention);
    const validRetention = retentionHours !== null;
    const [selected, setSelected] = useState("");
    const active = (members.data ?? []).filter(u => u.is_active);
    const tenant = typeof window === "undefined" ? null : localStorage.getItem("tabmail_tenant_id");
    const owners = active.filter(user => !!tenant && user.tenant_id === tenant);
    const directoryReady = !members.error && !members.isLoading && !members.isValidating && Array.isArray(members.data);
    // Keep an eligible selection during a pending refresh. A failed or settled
    // ineligible observation retires it, even while the personal field is hidden.
    const retireOwner = ownerSelection.scope !== scope || (ownerSelection.id !== "" &&
        (!!members.error || (directoryReady && !owners.some(user => user.id === ownerSelection.id))));
    if (retireOwner) setOwnerSelection({ scope, id: "" });
    const owner = retireOwner ? "" : ownerSelection.id;
    const draftSignature = JSON.stringify([boxLocal, kind, owner, retention]);
    useLayoutEffect(() => { currentDraft.current = draftSignature; }, [draftSignature]);
    const validOwner = directoryReady && owners.some(user => user.id === owner);
    const validCreate = Boolean(boxLocal) && (kind === "personal" ? validOwner : validRetention);
    const selectOwner = (id: string) => {
        if (scope !== sessionScope() || !directoryReady || (id !== "" && !owners.some(user => user.id === id))) return;
        draftIntent.current = {};
        setOwnerSelection({ scope, id });
    };
    const edit = (set: (value: string) => void, value: string) => {
        if (!owns(view)) return;
        draftIntent.current = {};
        set(value);
    };
    async function refreshBoxes(owner: object) {
        if (!owns(owner)) return;
        const readback = {};
        currentReadback.current = readback;
        // A bare SWR revalidation may resolve with stale cached data after a
        // failed GET. This promise confirms the post-create read actually ran.
        try {
            await boxes.mutate(async () => {
                const fresh = await workMailboxes();
                if (!owns(owner)) throw new DOMException("Mailbox page changed", "AbortError");
                return fresh;
            }, { revalidate: false });
            if (owns(owner) && currentReadback.current === readback) setCreationError(null);
        } catch (error) {
            // A later explicit retry owns the readback status, even if an
            // earlier GET settles last. SWR already orders the cached data.
            if (owns(owner) && currentReadback.current === readback) throw error;
        }
    }
    async function createMailbox() {
        if (!validCreate || !owns(view) || activeCreate.current?.view === view) return;
        const request = { view };
        const intent = draftIntent.current;
        // The ref closes the same-event double-click window before React
        // renders busy. A replacement session owns a distinct request slot.
        activeCreate.current = request;
        setPendingView(view);
        try {
            await company("/mailboxes", {
                method: "POST",
                body: {
                    local_part: boxLocal,
                    kind,
                    owner_user_id: kind === "personal" ? owner : undefined,
                    retention_hours: kind === "personal" ? 0 : retentionHours,
                },
            });
            if (!owns(view)) return;
            // Explicit edits (including ABA) and a retired owner selection
            // both supersede the submitted draft without losing its receipt.
            if (draftIntent.current === intent && currentDraft.current === draftSignature) {
                draftIntent.current = {};
                setBoxLocal("");
            }
            toast.success(t("私有邮箱已创建", "Private mailbox created"));
            try { await refreshBoxes(view); } catch {
                if (owns(view)) setCreationError({ view, error: new Error(t("邮箱已创建，但邮箱列表刷新失败。请重试加载以核对当前列表。", "The mailbox was created, but the mailbox list could not be refreshed. Retry loading to check the current list.")) });
            }
        } catch (error) {
            if (owns(view) && !(error instanceof DOMException && error.name === "AbortError")) toast.error(errorText(error));
        } finally {
            if (activeCreate.current === request) {
                activeCreate.current = null;
                if (owns(view)) setPendingView(null);
            }
        }
    }
    const mailbox = boxes.data?.find(v => v.mailbox.id === selected);
    return <div className="space-y-5"><LoadError error={readbackError || members.error || boxes.error} onRetry={() => {
        if (!owns(view)) return;
        void Promise.all([members.mutate(), refreshBoxes(view)]).catch(error => {
            if (owns(view)) setCreationError({ view, error: new Error(errorText(error)) });
        });
    }}/>
      <Section title={t("创建长期公司邮箱", "Create a company mailbox")}>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("邮箱用户名", "Mailbox local part")}>
            {(id) => (<input id={id} className={inputClass} value={boxLocal} onChange={(e) => edit(setBoxLocal, e.target.value)}/>)}
          </Field>
          <Field label={t("资源类型", "Resource type")}>
            {(id) => (<select id={id} className={inputClass} value={kind} onChange={(e) => edit(setKind, e.target.value)}>
                <option value="shared">
                  {t("公司共享邮箱", "Company shared mailbox")}
                </option>
                <option value="personal">
                  {t("员工个人邮箱", "Personal mailbox")}
                </option>
              </select>)}
          </Field>
          {kind === "personal" ? (<EmployeeField label={t("邮箱属主", "Mailbox owner")} value={owner} onChange={selectOwner} employees={owners} disabled={!directoryReady}/>) : (<Field label={t("保留小时数（0 为永久）", "Retention hours (0 = permanent)")}>
              {(id) => (<>
                <input id={id} className={inputClass} type="number" min={0} max={876000} step={1} value={retention} aria-invalid={!validRetention || undefined} aria-describedby={!validRetention ? `${id}-error` : undefined} onChange={(e) => edit(setRetention, e.target.value)}/>
                {!validRetention && <p id={`${id}-error`} role="alert" className="text-sm text-destructive">{t("请输入 0–876000 的整数小时数；0 表示永久保留。", "Enter a whole number of hours from 0 to 876000; 0 means permanent storage.")}</p>}
              </>)}
            </Field>)}
        </div>
        <ActionButton disabled={busy || !validCreate} onClick={() => void createMailbox()}>
          {t("创建邮箱", "Create mailbox")}
        </ActionButton>
      </Section>
      <Section title={t("邮箱授权与交接", "Mailbox grants and handover")}>
        <Field label={t("管理邮箱", "Manage mailbox")}>
          {(id) => (<select id={id} className={inputClass} value={selected} onChange={(e) => setSelected(e.target.value)}>
              <option value="">{t("请选择邮箱", "Select a mailbox")}</option>
              {(boxes.data ?? []).map((v) => (<option key={v.mailbox.id} value={v.mailbox.id}>
                  {v.mailbox.full_address} · {v.mailbox.kind ?? "legacy"}
                </option>))}
            </select>)}
        </Field>
        {mailbox && (<GrantEditor key={mailbox.mailbox.id} mailbox={mailbox} employees={active} refresh={() => boxes.mutate()}/>)}
        {mailbox && (<MailboxSendPolicyEditor key={`send-policy:${mailbox.mailbox.id}`} mailbox={mailbox} refresh={() => boxes.mutate()}/>)}
      </Section>
    {mailbox && <AccessExplanationPanel key={mailbox.mailbox.id} mailbox={mailbox.mailbox.id} employees={members.data ?? []} employeesReady={directoryReady} employeesError={members.error}/>}
    </div>;
}
