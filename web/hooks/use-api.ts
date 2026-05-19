import useSWR, { type SWRConfiguration } from "swr";
import type { SWRResponse } from "swr";

export function useAPI<T>(
  key: string | [string, ...unknown[]] | null,
  fetcher: () => Promise<T>,
  config?: SWRConfiguration<T>,
): SWRResponse<T> {
  return useSWR(key, fetcher, {
    revalidateOnFocus: false,
    ...config,
  });
}
