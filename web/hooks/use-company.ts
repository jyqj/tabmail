"use client";
import useSWR from "swr";
import { useAuth } from "@/contexts/auth-context";
import { companyState } from "@/lib/api/company";
export function useCompany() {
  const { user, tenantId, hydrated } = useAuth();
  return useSWR(
    user && hydrated ? ["company", user.id, tenantId] : null,
    companyState,
    { revalidateOnFocus: true, refreshInterval: 30000 },
  );
}
