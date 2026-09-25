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
    // Regular users cannot access admin pages
    if (
      level === "user" &&
      (pathname.startsWith("/admin") ||
        pathname.startsWith("/company") ||
        pathname.startsWith("/console"))
    ) {
      router.replace("/mail");
    }
    // Regular admins cannot access super_admin-only admin pages
    if (
      level === "admin" &&
      (pathname.startsWith("/company/recovery") ||
        pathname === "/admin" ||
        pathname.startsWith("/admin/monitor") ||
        pathname.startsWith("/admin/audit") ||
        pathname.startsWith("/admin/ingest") ||
        pathname.startsWith("/admin/webhooks") ||
        pathname.startsWith("/admin/tenants") ||
        pathname.startsWith("/admin/plans") ||
        pathname.startsWith("/admin/settings") ||
        pathname.startsWith("/admin/policy"))
    ) {
      router.replace("/company");
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

  if (level === "public") return null;
  if (
    level === "user" &&
    ["/admin", "/company", "/console"].some((prefix) =>
      pathname.startsWith(prefix),
    )
  )
    return null;
  if (
    level === "admin" &&
    (pathname === "/admin" ||
      [
        "/company/recovery",
        "/admin/monitor",
        "/admin/audit",
        "/admin/ingest",
        "/admin/webhooks",
        "/admin/tenants",
        "/admin/plans",
        "/admin/settings",
        "/admin/policy",
      ].some((prefix) => pathname.startsWith(prefix)))
  )
    return null;

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>{children}</SidebarInset>
    </SidebarProvider>
  );
}
