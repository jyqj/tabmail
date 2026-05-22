"use client";

import { useAuth } from "@/contexts/auth-context";
import { AppSidebar } from "@/components/layout/app-sidebar";
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import { Loader2 } from "lucide-react";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { level, hydrated } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (!hydrated) return;
    if (level === "public") router.replace("/");
    if (level === "mailbox") router.replace("/");
    // Regular users cannot access admin pages
    if (level === "user" && pathname.startsWith("/admin")) {
      router.replace("/console/domains");
    }
    // Tenant admins cannot access platform-only admin pages
    if (level === "tenant_admin" && (
      pathname === "/admin" ||
      pathname.startsWith("/admin/monitor") ||
      pathname.startsWith("/admin/audit") ||
      pathname.startsWith("/admin/ingest") ||
      pathname.startsWith("/admin/webhooks") ||
      pathname.startsWith("/admin/tenants") ||
      pathname.startsWith("/admin/plans") ||
      pathname.startsWith("/admin/settings") ||
      pathname.startsWith("/admin/policy")
    )) {
      router.replace("/admin/users");
    }
  }, [hydrated, level, pathname, router]);

  if (!hydrated) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="flex flex-col items-center gap-3 text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin" />
        </div>
      </div>
    );
  }

  if (level === "public" || level === "mailbox") return null;

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>{children}</SidebarInset>
    </SidebarProvider>
  );
}
