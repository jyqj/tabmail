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
export function MailboxAdmin() {
    const t = useText();
    const { busy, run } = useAction();
    const members = useAPI("company-employees", allEmployees);
    const boxes = useAPI("company-managed-mailboxes", workMailboxes);
    const [boxLocal, setBoxLocal] = useState("");
    const [kind, setKind] = useState("shared");
    const [owner, setOwner] = useState("");
    const [retention, setRetention] = useState(0);
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
              {(id) => (<input id={id} className={inputClass} type="number" min={0} max={876000} step={1} value={retention} onChange={(e) => setRetention(Number(e.target.value))}/>)}
            </Field>)}
        </div>
        <ActionButton disabled={busy ||
            !boxLocal ||
            (kind === "personal" && !owner) ||
            !Number.isInteger(retention) ||
            retention < 0} onClick={() => run(async () => {
            await company("/mailboxes", {
                method: "POST",
                body: {
                    local_part: boxLocal,
                    kind,
                    owner_user_id: kind === "personal" ? owner : undefined,
                    retention_hours: kind === "personal" ? 0 : retention,
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
