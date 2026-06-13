import { useEffect } from "react";
import { toast } from "sonner";
import type { SWRConfiguration, SWRResponse } from "swr";
import { useAPI } from "@/hooks/use-api";
import { useI18n } from "@/lib/i18n";

/**
 * Standard CRUD list-page data hook: identical SWR semantics to useAPI,
 * plus the shared "toast a localized error when loading fails" effect
 * that every plain list page repeats.
 */
export function useCRUDPage<T>(
  key: string | [string, ...unknown[]] | null,
  fetcher: () => Promise<T>,
  errorKey: string,
  config?: SWRConfiguration<T>,
): SWRResponse<T> {
  const { t } = useI18n();
  const swr = useAPI<T>(key, fetcher, config);
  const { error } = swr;

  useEffect(() => {
    if (error) toast.error(t(errorKey));
  }, [error, errorKey, t]);

  return swr;
}
