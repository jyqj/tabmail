"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { ActionButton, Field, inputClass, LoadError, Section, useAction, useText } from "@/components/company/common";
import { company, allEmployees, workMailboxes } from "@/lib/company";
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
    const { busy, run } = useAction();
    const members = useAPI("company-employees", allEmployees);
    const boxes = useAPI("company-managed-mailboxes", workMailboxes);
    const [boxLocal, setBoxLocal] = useState("");
    const [kind, setKind] = useState("shared");
    const [owner, setOwner] = useState("");
    const [retention, setRetention] = useState("0");
    // An empty field is unfinished input, not an instruction for permanence.
    const retentionHours = parseRetentionHours(retention);
    const validRetention = retentionHours !== null;
    const validCreate = Boolean(boxLocal) && (kind === "personal" ? Boolean(owner) : validRetention);
    const [selected, setSelected] = useState("");
    const active = (members.data ?? []).filter(u => u.is_active);
    const mailbox = boxes.data?.find(v => v.mailbox.id === selected);
    return <div className="space-y-5"><LoadError error={members.error || boxes.error} onRetry={() => { void members.mutate(); void boxes.mutate(); }}/>
      <Section title={t("创建长期公司邮箱", "Create a company mailbox")}>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("邮箱用户名", "Mailbox local part")}>
            {(id) => (<input id={id} className={inputClass} value={boxLocal} onChange={(e) => setBoxLocal(e.target.value)}/>)}
          </Field>
          <Field label={t("资源类型", "Resource type")}>
            {(id) => (<select id={id} className={inputClass} value={kind} onChange={(e) => setKind(e.target.value)}>
                <option value="shared">
                  {t("公司共享邮箱", "Company shared mailbox")}
                </option>
                <option value="personal">
                  {t("员工个人邮箱", "Personal mailbox")}
                </option>
              </select>)}
          </Field>
          {kind === "personal" ? (<EmployeeField label={t("邮箱属主", "Mailbox owner")} value={owner} onChange={setOwner} employees={active}/>) : (<Field label={t("保留小时数（0 为永久）", "Retention hours (0 = permanent)")}>
              {(id) => (<>
                <input id={id} className={inputClass} type="number" min={0} max={876000} step={1} value={retention} aria-invalid={!validRetention || undefined} aria-describedby={!validRetention ? `${id}-error` : undefined} onChange={(e) => setRetention(e.target.value)}/>
                {!validRetention && <p id={`${id}-error`} role="alert" className="text-sm text-destructive">{t("请输入 0–876000 的整数小时数；0 表示永久保留。", "Enter a whole number of hours from 0 to 876000; 0 means permanent storage.")}</p>}
              </>)}
            </Field>)}
        </div>
        <ActionButton disabled={busy || !validCreate} onClick={() => run(async () => {
            if (!validCreate) return;
            await company("/mailboxes", {
                method: "POST",
                body: {
                    local_part: boxLocal,
                    kind,
                    owner_user_id: kind === "personal" ? owner : undefined,
                    retention_hours: kind === "personal" ? 0 : retentionHours,
                },
            });
            setBoxLocal("");
            await boxes.mutate();
            toast.success(t("私有邮箱已创建", "Private mailbox created"));
        })}>
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
    {mailbox && <AccessExplanationPanel key={mailbox.mailbox.id} mailbox={mailbox.mailbox.id} employees={members.data ?? []}/>}
    </div>;
}
