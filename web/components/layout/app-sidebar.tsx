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
  Webhook,
  Boxes,
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
    { href: "/admin/ingest", label: t("sidebar.ingest"), icon: Boxes },
    { href: "/admin/webhooks", label: t("sidebar.webhooks"), icon: Webhook },
  ];

  return (
    <Sidebar className="border-r border-border/40 backdrop-blur-3xl bg-background/60">
      <SidebarHeader className="pb-4 pt-6 px-4">
        <div className="flex items-center gap-3 px-2 py-1">
          <div className="relative flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-[0_0_15px_rgba(var(--color-primary),0.5)]">
            <Mail className="h-4 w-4" />
          </div>
          <span className="font-heading text-lg font-bold tracking-tight">TabMail</span>
        </div>
      </SidebarHeader>

      <SidebarContent className="px-3">
        <SidebarGroup>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton 
                render={<Link href="/" />} 
                isActive={pathname === "/"}
                className="transition-all hover:bg-muted/50 data-[active=true]:bg-primary/10 data-[active=true]:text-primary data-[active=true]:font-medium"
              >
                <Home className="h-4 w-4" />
                <span>{t("sidebar.home")}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroup>

        <SidebarSeparator className="my-2 bg-border/40" />

        <SidebarGroup>
          <SidebarGroupLabel className="text-xs font-semibold uppercase tracking-wider text-muted-foreground/70 mb-1">{t("sidebar.console")}</SidebarGroupLabel>
          <SidebarMenu>
            {consoleItems.map((item) => (
              <SidebarMenuItem key={item.href}>
                <SidebarMenuButton
                  render={<Link href={item.href} />}
                  isActive={pathname.startsWith(item.href)}
                  className="transition-all hover:bg-muted/50 data-[active=true]:bg-primary/10 data-[active=true]:text-primary data-[active=true]:font-medium"
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
            <SidebarSeparator className="my-2 bg-border/40" />
            <SidebarGroup>
              <SidebarGroupLabel className="gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground/70 mb-1">
                <Shield className="h-3 w-3 text-destructive/80" />
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
                      className="transition-all hover:bg-muted/50 data-[active=true]:bg-primary/10 data-[active=true]:text-primary data-[active=true]:font-medium"
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

      <SidebarFooter className="p-4 border-t border-border/40 bg-background/40">
        <div className="flex items-center justify-between px-2">
          <ThemeToggle />
          <Button variant="ghost" size="icon" onClick={logout} className="h-8 w-8 hover:bg-destructive/10 hover:text-destructive transition-colors">
            <LogOut className="h-4 w-4" />
          </Button>
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
