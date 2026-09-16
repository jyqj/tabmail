"use client";
import type { AdminUser } from "@/lib/types";
import { Field, inputClass, useText } from "./common";
export function EmployeeField({
  label,
  value,
  onChange,
  employees,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  employees: AdminUser[];
}) {
  const t = useText();
  return (
    <Field label={label}>
      {(id) => (
        <select
          id={id}
          className={inputClass}
          value={value}
          onChange={(e) => onChange(e.target.value)}
        >
          <option value="">{t("请选择成员", "Select a member")}</option>
          {employees.map((v) => (
            <option key={v.id} value={v.id}>
              {v.display_name} · {v.email}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
}
