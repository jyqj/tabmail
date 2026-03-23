"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "@/contexts/auth-context";
import { useI18n } from "@/lib/i18n";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarFooter,
  SidebarSeparator,
} from "@/components/ui/sidebar";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import {
  Mail,
  Globe,
  Inbox,
  Users,
  CreditCard,
  BarChart3,
  Radar,
  SlidersHorizontal,
  Home,
  LogOut,
  Shield,
  ClipboardList,
} from "lucide-react";

export function AppSidebar() {
  const pathname = usePathname();
  const { level, logout } = useAuth();
  const { t } = useI18n();

  const consoleItems = [
    { href: "/console/domains", label: t("sidebar.domains"), icon: Globe },
    { href: "/console/mailboxes", label: t("sidebar.mailboxes"), icon: Inbox },
  ];

  const adminItems = [
    { href: "/admin", label: t("sidebar.statistics"), icon: BarChart3 },
    { href: "/admin/monitor", label: t("sidebar.monitor"), icon: Radar },
    { href: "/admin/policy", label: t("sidebar.smtpPolicy"), icon: SlidersHorizontal },
    { href: "/admin/tenants", label: t("sidebar.tenants"), icon: Users },
    { href: "/admin/plans", label: t("sidebar.plans"), icon: CreditCard },
    { href: "/admin/audit", label: t("sidebar.audit"), icon: ClipboardList },
  ];

  return (
    <Sidebar>
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1">
          <Mail className="h-5 w-5 text-primary" />
          <span className="font-semibold tracking-tight">TabMail</span>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton render={<Link href="/" />} isActive={pathname === "/"}>
                <Home className="h-4 w-4" />
                <span>{t("sidebar.home")}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroup>

        <SidebarSeparator />

        <SidebarGroup>
          <SidebarGroupLabel>{t("sidebar.console")}</SidebarGroupLabel>
          <SidebarMenu>
            {consoleItems.map((item) => (
              <SidebarMenuItem key={item.href}>
                <SidebarMenuButton
                  render={<Link href={item.href} />}
                  isActive={pathname.startsWith(item.href)}
                >
                  <item.icon className="h-4 w-4" />
                  <span>{item.label}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </SidebarGroup>

        {level === "admin" && (
          <>
            <SidebarSeparator />
            <SidebarGroup>
              <SidebarGroupLabel className="gap-1.5">
                <Shield className="h-3 w-3" />
                {t("sidebar.admin")}
              </SidebarGroupLabel>
              <SidebarMenu>
                {adminItems.map((item) => (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                      render={<Link href={item.href} />}
                      isActive={
                        item.href === "/admin"
                          ? pathname === "/admin"
                          : pathname.startsWith(item.href)
                      }
                    >
                      <item.icon className="h-4 w-4" />
                      <span>{item.label}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroup>
          </>
        )}
      </SidebarContent>

      <SidebarFooter>
        <div className="flex items-center justify-between px-2">
          <ThemeToggle />
          <Button variant="ghost" size="icon" onClick={logout} className="h-8 w-8">
            <LogOut className="h-4 w-4" />
          </Button>
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
