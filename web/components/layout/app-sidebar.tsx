"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "@/contexts/auth-context";
import {
  canCreateAPIKeys,
  canSend,
  isAdminLevel,
  isSuperAdminLevel,
} from "@/lib/permissions";
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
  Settings2,
  Send,
  KeyRound,
} from "lucide-react";

export function AppSidebar() {
  const pathname = usePathname();
  const { level, logout, permissions } = useAuth();
  const { t } = useI18n();
  // UX-only gates; the backend authz seam is authoritative.
  const adminLevel = isAdminLevel(level);
  const showAPIKeys = canCreateAPIKeys(level, permissions);
  const showSend = canSend(level, permissions);
  const webhookPermissions = permissions as
    | (typeof permissions & {
        can_manage_webhooks?: boolean;
        can_create_webhooks?: boolean;
        can_manage_webhook_endpoints?: boolean;
        can_create_webhook_endpoints?: boolean;
        scopes?: string[];
        webhook_scopes?: string[];
      })
    | null;
  const canUseWebhooks =
    adminLevel ||
    webhookPermissions?.can_manage_webhooks === true ||
    webhookPermissions?.can_create_webhooks === true ||
    webhookPermissions?.can_manage_webhook_endpoints === true ||
    webhookPermissions?.can_create_webhook_endpoints === true ||
    webhookPermissions?.scopes?.some((scope) => scope.startsWith("webhooks:")) === true ||
    webhookPermissions?.webhook_scopes?.some((scope) => scope.startsWith("webhooks:")) === true;

  const consoleItems = [
    { href: "/console/domains", label: t("sidebar.domains"), icon: Globe },
    { href: "/console/mailboxes", label: t("sidebar.mailboxes"), icon: Inbox },
    // Fail closed while permissions are loading/unavailable.
    ...(showAPIKeys
      ? [{ href: "/console/keys", label: t("sidebar.apiKeys"), icon: Settings2 }]
      : []),
    ...(showSend
      ? [{ href: "/console/outbound", label: t("sidebar.outbound"), icon: Send }]
      : []),
    ...(showSend
      ? [{ href: "/console/send-identities", label: t("sidebar.sendIdentities"), icon: KeyRound }]
      : []),
    // Fail closed: current EffectivePermission has no webhook flag, so plain
    // users do not see this unless a future explicit capability is present.
    ...(canUseWebhooks
      ? [{ href: "/console/webhooks", label: t("sidebar.webhookEndpoints"), icon: Webhook }]
      : []),
  ];

  const isPlatformAdmin = isSuperAdminLevel(level);

  // Shared admin items (accessible by both super_admin and admin)
  const sharedAdminItems = [
    { href: "/admin/users", label: t("sidebar.users"), icon: Users },
    { href: "/admin/permissions", label: t("sidebar.permissions"), icon: Shield },
    { href: "/admin/domains", label: t("sidebar.domainResources"), icon: Globe },
  ];

  // Platform-only operations expose global telemetry/audit data.
  const platformOpsItems = [
    { href: "/admin", label: t("sidebar.statistics"), icon: BarChart3 },
    { href: "/admin/monitor", label: t("sidebar.monitor"), icon: Radar },
    { href: "/admin/audit", label: t("sidebar.audit"), icon: ClipboardList },
    { href: "/admin/ingest", label: t("sidebar.ingest"), icon: Boxes },
    { href: "/admin/webhooks", label: t("sidebar.webhooks"), icon: Webhook },
  ];

  // Platform-only admin items (only super_admin)
  const platformAdminItems = [
    { href: "/admin/policy", label: t("sidebar.smtpPolicy"), icon: SlidersHorizontal },
    { href: "/admin/tenants", label: t("sidebar.tenants"), icon: Users },
    { href: "/admin/plans", label: t("sidebar.plans"), icon: CreditCard },
    { href: "/admin/settings", label: t("sidebar.settings"), icon: Settings2 },
  ];

  const adminItems = isPlatformAdmin
    ? [...platformOpsItems, ...sharedAdminItems, ...platformAdminItems]
    : sharedAdminItems;

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
                className="transition-all hover:bg-muted/50 data-[active=true]:bg-primary/10 data-[active=true]:text-primary data-[active=true]:font-medium data-[active=true]:border-l-2 data-[active=true]:border-l-primary"
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
                  className="transition-all hover:bg-muted/50 data-[active=true]:bg-primary/10 data-[active=true]:text-primary data-[active=true]:font-medium data-[active=true]:border-l-2 data-[active=true]:border-l-primary"
                >
                  <item.icon className="h-4 w-4" />
                  <span>{item.label}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </SidebarGroup>

        {adminLevel && (
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
                      className="transition-all hover:bg-muted/50 data-[active=true]:bg-primary/10 data-[active=true]:text-primary data-[active=true]:font-medium data-[active=true]:border-l-2 data-[active=true]:border-l-primary"
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
