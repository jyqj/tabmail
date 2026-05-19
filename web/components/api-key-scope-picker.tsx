"use client";

import { Button } from "@/components/ui/button";
import {
  ALL_API_KEY_SCOPES,
  API_KEY_SCOPE_OPTIONS,
  DEFAULT_API_KEY_SCOPES,
} from "@/lib/api-key-scopes";
import { useI18n } from "@/lib/i18n";

interface APIKeyScopePickerProps {
  value: string[];
  onChange: (scopes: string[]) => void;
  disabled?: boolean;
}

export function APIKeyScopePicker({ value, onChange, disabled }: APIKeyScopePickerProps) {
  const { t } = useI18n();
  const selected = new Set(value);
  const toggle = (scope: string) => {
    if (selected.has(scope)) {
      onChange(value.filter((item) => item !== scope));
      return;
    }
    onChange([...value, scope]);
  };

  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="text-sm font-medium">{t("apiKeyScopes.title")}</div>
          <p className="text-xs text-muted-foreground">
            {t("apiKeyScopes.desc")}
          </p>
        </div>
        <div className="flex shrink-0 gap-1">
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={disabled}
            onClick={() => onChange([...DEFAULT_API_KEY_SCOPES])}
          >
            {t("apiKeyScopes.readOnly")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={disabled}
            onClick={() => onChange([...ALL_API_KEY_SCOPES])}
          >
            {t("apiKeyScopes.all")}
          </Button>
        </div>
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        {API_KEY_SCOPE_OPTIONS.map((scope) => (
          <label
            key={scope.value}
            className="flex cursor-pointer items-center gap-2 rounded-md border bg-background px-2.5 py-2 text-xs"
          >
            <input
              type="checkbox"
              className="h-3.5 w-3.5 rounded border-border"
              checked={selected.has(scope.value)}
              disabled={disabled}
              onChange={() => toggle(scope.value)}
            />
            <span className="font-mono">{scope.value}</span>
          </label>
        ))}
      </div>
    </div>
  );
}
