import useSWR, { type SWRConfiguration, type SWRResponse } from "swr";
import { useSessionScope } from "@/lib/session";

export function useAPI<T>(
  key: string | [string, ...unknown[]] | null,
  fetcher: () => Promise<T>,
  config?: SWRConfiguration<T>,
): SWRResponse<T> {
  const scope = useSessionScope();
  return useSWR(key === null ? null : ["session", scope, key], fetcher, {
    revalidateOnFocus: true,
    ...config,
    keepPreviousData: false,
  });
}
