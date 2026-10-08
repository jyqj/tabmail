"use client";
import type { AdminUser } from "@/lib/types";
import { Field, inputClass, useText } from "./common";
export function EmployeeField({
  label,
  value,
  onChange,
  employees,
  disabled = false,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  employees: AdminUser[];
  disabled?: boolean;
}) {
  const t = useText();
  return (
    <Field label={label}>
      {(id) => (
        <select
          id={id}
          className={inputClass}
          value={value}
          disabled={disabled}
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
