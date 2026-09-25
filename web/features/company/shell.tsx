"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { useText } from "@/components/company/common";
export function CompanyShell({ title, children }: {
    title: [
        string,
        string
    ];
    children: ReactNode;
}) {
    const t = useText();
    const path = usePathname();
    return <main className="mx-auto w-full max-w-6xl space-y-5 p-4 md:p-7"><header><h1 className="text-2xl font-semibold">{t(...title)}</h1><p className="mt-2 text-sm text-muted-foreground">{t("管理权限不自动授予员工邮件正文访问权。", "Administrative authority does not grant employee message-content access.")}</p></header><nav aria-label={t("公司管理导航", "Company navigation")} className="flex flex-wrap gap-2">{[["", t("概览", "Overview")], ["employees", t("员工", "Employees")], ["mailboxes", t("邮箱与授权", "Mailboxes and grants")], ["domains", t("域名与策略", "Domains and policy")], ["templates", t("发送模板", "Templates")], ["access", t("成员权限", "Employee permissions")], ["profiles", t("权限配置", "Permission profiles")], ["audit", t("管理审计", "Audit")]].map(([route, label]) => { const href = route ? `/company/${route}` : "/company"; return <Link key={href} aria-current={path === href ? "page" : undefined} className={`rounded border px-3 py-2 text-sm ${path === href ? "bg-muted" : ""}`} href={href}>{label}</Link>; })}</nav>{children}</main>;
}
