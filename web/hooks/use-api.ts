import useSWR, { type SWRConfiguration } from "swr";
import type { SWRResponse } from "swr";
import useSWRInfinite, {
  type SWRInfiniteConfiguration,
  type SWRInfiniteKeyLoader,
  type SWRInfiniteResponse,
} from "swr/infinite";

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

// useAPIInfinite is the keyset-pagination counterpart of useAPI: getKey derives
// each page key from the previous page (typically its meta.next_cursor) and
// returns null when the list ends.
export function useAPIInfinite<T>(
  getKey: SWRInfiniteKeyLoader<T>,
  fetcher: (key: readonly unknown[]) => Promise<T>,
  config?: SWRInfiniteConfiguration<T>,
): SWRInfiniteResponse<T> {
  return useSWRInfinite(getKey, fetcher, {
    revalidateOnFocus: false,
    ...config,
  });
}
