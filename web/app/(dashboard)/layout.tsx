"use client";

import { useAuth } from "@/contexts/auth-context";
import { AppSidebar } from "@/components/layout/app-sidebar";
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { level } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (level === "public") router.replace("/");
    if (level === "mailbox") router.replace("/");
    if (level === "tenant" && pathname.startsWith("/admin")) {
      router.replace("/console/domains");
    }
  }, [level, pathname, router]);

  if (level === "public" || level === "mailbox") return null;

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>{children}</SidebarInset>
    </SidebarProvider>
  );
}
