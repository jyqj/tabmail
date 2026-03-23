"use client";

import {
  createContext,
  useContext,
  useEffect,
  useCallback,
  useSyncExternalStore,
  type ReactNode,
} from "react";

export type Locale = "zh" | "en";

const STORAGE_KEY = "tabmail-locale";

type Messages = Record<string, string>;

const zh: Messages = {
  // Header
  "header.console": "控制台",
  "header.inbox": "收件箱",
  "header.admin": "管理后台",
  "header.docs": "API 文档",
  "header.healthy": "正常",
  "header.down": "离线",
  "header.checking": "检测中",
  "header.nav": "导航菜单",

  // Home — hero
  "home.badge": "自托管 · 开源 · API 优先",
  "home.title1": "临时邮件，",
  "home.title2": "自主掌控。",
  "home.desc": "支持自定义域名、多租户隔离和简洁 REST API 的临时邮件系统。部署在你自己的服务器——无需注册，无第三方依赖。",
  "home.placeholder": "anything@yourdomain.com",
  "home.openInbox": "打开收件箱",
  "home.random": "随机生成地址",

  // Home — how it works
  "home.howItWorks": "工作流程",
  "home.threeSteps": "三步启用临时邮件",
  "home.step01": "部署配置",
  "home.step01Desc": "Docker 或裸机自托管。绑定域名，设置保留规则与访问模式。",
  "home.step02": "接收邮件",
  "home.step02Desc": "SMTP 即时收取。精确、通配或序列路由解析到对应邮箱。",
  "home.step03": "随时查阅",
  "home.step03Desc": "在 Web 界面打开收件箱，或通过 REST API 拉取。SSE 实时推送保持同步。",

  // Home — features
  "home.features": "核心特性",
  "home.featuresTitle": "为开发者和运维打造",
  "home.featuresDesc": "生产级临时邮件系统所需的一切。",
  "home.feat.domains": "自定义域名",
  "home.feat.domainsDesc": "绑定任意域名。配置通配和序列路由，批量收取子域邮件。",
  "home.feat.access": "灵活访问控制",
  "home.feat.accessDesc": "按邮箱设置公开、Token 或 API Key 访问。细粒度控制。",
  "home.feat.cleanup": "自动清理",
  "home.feat.cleanupDesc": "按邮箱、路由、租户或方案配置保留策略。四级优先级级联。",
  "home.feat.tenancy": "多租户",
  "home.feat.tenancyDesc": "完整的租户隔离，含方案、覆盖、API Key 和范围权限。",
  "home.feat.perf": "高性能",
  "home.feat.perfDesc": "Go 后端，每连接独立协程 SMTP。Redis 速率限制。为规模而生。",
  "home.feat.api": "API 优先",
  "home.feat.apiDesc": "OpenAPI 3.1 规范为唯一真实来源。Swagger UI、ReDoc 和类型化 SDK。",

  // Home — CTA
  "home.ctaTitle": "准备好掌控你的邮件管线了吗？",
  "home.ctaDesc": "部署 TabMail，绑定域名，几分钟内开始收取邮件。",
  "home.ctaTry": "立即试用",
  "home.ctaDocs": "阅读文档",

  // Home — footer
  "home.tagline": "自托管临时邮件系统",

  // Home — mockup
  "mock.inbox": "收件箱",
  "mock.sender1": "GitHub",
  "mock.subject1": "你的验证码：839201",
  "mock.time1": "刚刚",
  "mock.sender2": "Stripe",
  "mock.subject2": "API Key 轮换提醒",
  "mock.time2": "2 分钟前",
  "mock.sender3": "TabMail",
  "mock.subject3": "欢迎使用你的新邮箱",
  "mock.time3": "5 分钟前",

  // Inbox page
  "inbox.messages": "{count} 封邮件",
  "inbox.newCount": "{count} 封新邮件",
  "inbox.live": "实时连接",
  "inbox.polling": "轮询中",
  "inbox.refresh": "刷新",
  "inbox.purge": "清空",
  "inbox.logout": "退出",
  "inbox.authTitle": "需要身份验证",
  "inbox.authDesc": "此收件箱配置为 Token 访问模式，请输入邮箱密码解锁。",
  "inbox.password": "邮箱密码",
  "inbox.connecting": "连接中...",
  "inbox.unlock": "解锁",
  "inbox.loading": "加载收件箱...",
  "inbox.selectMsg": "选择邮件查看",
  "inbox.selectMsgDesc": "从左侧列表选择一封邮件阅读完整内容",

  // Message list
  "msgList.empty": "暂无邮件",
  "msgList.emptyDesc": "发送到此地址的邮件将自动显示在这里",
  "msgList.new": "新",
  "msgList.unknown": "（未知发件人）",
  "msgList.noSubject": "（无主题）",

  // Message detail
  "msgDetail.loading": "加载中...",
  "msgDetail.html": "HTML",
  "msgDetail.text": "纯文本",
  "msgDetail.source": "源码",
  "msgDetail.noHtml": "无 HTML 内容",
  "msgDetail.noText": "无文本内容",
  "msgDetail.loadingSource": "加载源码...",
  "msgDetail.back": "返回",

  // Auth dialog
  "auth.connect": "连接",
  "auth.title": "身份验证",
  "auth.desc": "通过邮箱 Token、租户 API Key 或管理员密钥连接。",
  "auth.mailbox": "邮箱",
  "auth.apiKey": "API Key",
  "auth.admin": "管理员",
  "auth.mailboxAddr": "邮箱地址",
  "auth.mailboxPwd": "邮箱密码",
  "auth.mailboxAddrPh": "secure@mail.example.com",
  "auth.mailboxPwdPh": "输入邮箱密码",
  "auth.tenantKey": "租户 API Key",
  "auth.tenantKeyPh": "tbm_...",
  "auth.adminKey": "管理员密钥",
  "auth.adminKeyPh": "输入管理员密钥",
  "auth.tenantId": "租户 ID（Console/Admin 代理）",
  "auth.tenantIdPh": "可选的租户 UUID",
  "auth.level.admin": "管理员",
  "auth.level.tenant": "租户",
  "auth.level.mailbox": "邮箱",

  // Settings
  "settings.title": "设置",
  "settings.desc": "自定义显示和行为偏好",
  "settings.language": "语言",
  "settings.theme": "主题",
  "settings.themeSystem": "跟随系统",
  "settings.themeLight": "浅色",
  "settings.themeDark": "深色",
  "settings.autoRefresh": "自动刷新",
  "settings.autoRefreshDesc": "定时检查新邮件",
  "settings.refreshInterval": "刷新间隔",
  "settings.preferSSE": "优先使用 SSE",
  "settings.preferSSEDesc": "通过服务端推送实时接收新邮件",
  "settings.defaultTab": "默认邮件视图",
  "settings.timeFormat": "时间格式",
  "settings.timeRelative": "相对时间",
  "settings.timeAbsolute": "绝对时间",

  // FAQ
  "faq.title": "常见问题",
  "faq.q1": "Public inbox 和 Token inbox 有什么区别？",
  "faq.a1": "Public inbox 无需密码，任何知道地址的人都可以查看邮件。Token inbox 需要通过邮箱密码签发 token 才能访问，适合需要隐私保护的场景。你可以在路由配置中为每个邮箱设置访问模式。",
  "faq.q2": "如何获取 Mailbox Token？",
  "faq.a2": "在收件箱页面输入邮箱密码即可签发临时 token。你也可以通过 REST API 调用 POST /api/v1/token 获取。Token 有效期由后端配置决定。",
  "faq.q3": "如何接入自定义域名？",
  "faq.a3": "在租户控制台添加域名，按提示配置 MX、TXT、SPF 等 DNS 记录。验证通过后，该域名下的邮件会自动路由到 TabMail。支持通配和序列匹配。",
  "faq.q4": "保留策略和自动清理是怎么运作的？",
  "faq.a4": "TabMail 支持四级保留策略级联：方案 → 租户覆盖 → 路由覆盖 → 邮箱覆盖。过期邮件由后台任务自动清理，你也可以在收件箱中手动清空。",
  "faq.q5": "Tenant 和 Admin 分别是什么角色？",
  "faq.a5": "Admin 拥有全局管理权限：系统监控、SMTP 策略、租户管理和方案配置。Tenant 管理自己范围内的域名、路由和邮箱。普通用户通过 Mailbox Token 或 Public 模式访问收件箱。",

  // Inbox enhanced
  "inbox.everyNSec": "每 {sec} 秒",
  "inbox.paused": "已暂停",

  // Auth enhanced
  "auth.copyKey": "复制密钥",
  "auth.copyAddr": "复制地址",
  "auth.keyCopied": "已复制",

  // Toast
  "toast.copied": "地址已复制",
  "toast.tokenIssued": "邮箱 Token 已签发",
  "toast.authFailed": "认证失败",
  "toast.loadFailed": "加载邮件失败",
  "toast.deleteFailed": "删除失败",
  "toast.deleted": "邮件已删除",
  "toast.allDeleted": "所有邮件已删除",
  "toast.purgeFailed": "清空失败",
  "toast.adminOk": "管理员密钥已配置",
  "toast.apiKeyOk": "API Key 已配置",
};

const en: Messages = {
  // Header
  "header.console": "Console",
  "header.inbox": "Inbox",
  "header.admin": "Admin",
  "header.docs": "API Docs",
  "header.healthy": "Healthy",
  "header.down": "Down",
  "header.checking": "Checking",
  "header.nav": "Navigation",

  // Home — hero
  "home.badge": "Self-hosted · Open source · API-first",
  "home.title1": "Disposable email,",
  "home.title2": "your infrastructure.",
  "home.desc": "Temporary email with custom domains, multi-tenancy, and a clean REST API. Deploy on your own server — no sign-up, no third-party dependency.",
  "home.placeholder": "anything@yourdomain.com",
  "home.openInbox": "Open Inbox",
  "home.random": "Generate random address",

  // Home — how it works
  "home.howItWorks": "How it works",
  "home.threeSteps": "Three steps to disposable email",
  "home.step01": "Deploy & configure",
  "home.step01Desc": "Self-host with Docker or bare metal. Bind your domains, set retention rules and access modes.",
  "home.step02": "Receive email",
  "home.step02Desc": "SMTP server accepts mail instantly. Routes resolve to mailboxes via exact, wildcard, or sequence match.",
  "home.step03": "Read anywhere",
  "home.step03Desc": "Open any inbox in the web UI — or pull messages via REST API. Real-time SSE push keeps you in sync.",

  // Home — features
  "home.features": "Features",
  "home.featuresTitle": "Built for developers & operators",
  "home.featuresDesc": "Everything you need for a production-grade temporary email system.",
  "home.feat.domains": "Custom Domains",
  "home.feat.domainsDesc": "Bind any domain. Configure wildcard and sequence routes for batch subdomain emails.",
  "home.feat.access": "Flexible Access",
  "home.feat.accessDesc": "Public, token, or API-key access per mailbox. Fine-grained control for every use case.",
  "home.feat.cleanup": "Auto Cleanup",
  "home.feat.cleanupDesc": "Configurable retention per mailbox, route, tenant, or plan. 4-level priority cascade.",
  "home.feat.tenancy": "Multi-Tenancy",
  "home.feat.tenancyDesc": "Full tenant isolation with plans, overrides, API keys, and scoped permissions.",
  "home.feat.perf": "High Performance",
  "home.feat.perfDesc": "Go backend with goroutine-per-connection SMTP. Redis rate limiting. Built for scale.",
  "home.feat.api": "API First",
  "home.feat.apiDesc": "OpenAPI 3.1 spec as single source of truth. Swagger UI, ReDoc, and typed SDKs.",

  // Home — CTA
  "home.ctaTitle": "Ready to own your email pipeline?",
  "home.ctaDesc": "Deploy TabMail, bind a domain, and start receiving email in minutes.",
  "home.ctaTry": "Try an inbox now",
  "home.ctaDocs": "Read the docs",

  // Home — footer
  "home.tagline": "Self-hosted temporary email system",

  // Home — mockup
  "mock.inbox": "Inbox",
  "mock.sender1": "GitHub",
  "mock.subject1": "Your verification code: 839201",
  "mock.time1": "just now",
  "mock.sender2": "Stripe",
  "mock.subject2": "API key rotation reminder",
  "mock.time2": "2 min ago",
  "mock.sender3": "TabMail",
  "mock.subject3": "Welcome to your new mailbox",
  "mock.time3": "5 min ago",

  // Inbox page
  "inbox.messages": "{count} message{count, plural}",
  "inbox.newCount": "{count} new",
  "inbox.live": "Live",
  "inbox.polling": "Polling",
  "inbox.refresh": "Refresh",
  "inbox.purge": "Purge",
  "inbox.logout": "Logout",
  "inbox.authTitle": "Authentication required",
  "inbox.authDesc": "This inbox is configured as token access. Enter the mailbox password to unlock.",
  "inbox.password": "Mailbox password",
  "inbox.connecting": "Connecting...",
  "inbox.unlock": "Unlock",
  "inbox.loading": "Loading inbox...",
  "inbox.selectMsg": "Select a message to read",
  "inbox.selectMsgDesc": "Choose from the list on the left to view the full message",

  // Message list
  "msgList.empty": "No messages yet",
  "msgList.emptyDesc": "Emails sent to this address will appear here automatically",
  "msgList.new": "New",
  "msgList.unknown": "(unknown)",
  "msgList.noSubject": "(no subject)",

  // Message detail
  "msgDetail.loading": "Loading message...",
  "msgDetail.html": "HTML",
  "msgDetail.text": "Text",
  "msgDetail.source": "Source",
  "msgDetail.noHtml": "No HTML content",
  "msgDetail.noText": "No text content",
  "msgDetail.loadingSource": "Loading source...",
  "msgDetail.back": "Back",

  // Auth dialog
  "auth.connect": "Connect",
  "auth.title": "Authenticate",
  "auth.desc": "Connect with a mailbox token flow, tenant API key, or admin key.",
  "auth.mailbox": "Mailbox",
  "auth.apiKey": "API Key",
  "auth.admin": "Admin",
  "auth.mailboxAddr": "Mailbox Address",
  "auth.mailboxPwd": "Mailbox Password",
  "auth.mailboxAddrPh": "secure@mail.example.com",
  "auth.mailboxPwdPh": "Enter mailbox password",
  "auth.tenantKey": "Tenant API Key",
  "auth.tenantKeyPh": "tbm_...",
  "auth.adminKey": "Admin Key",
  "auth.adminKeyPh": "Enter admin key",
  "auth.tenantId": "Tenant ID for Console/Admin proxy",
  "auth.tenantIdPh": "optional tenant uuid",
  "auth.level.admin": "Admin",
  "auth.level.tenant": "Tenant",
  "auth.level.mailbox": "Mailbox",

  // Settings
  "settings.title": "Settings",
  "settings.desc": "Customize display and behavior preferences",
  "settings.language": "Language",
  "settings.theme": "Theme",
  "settings.themeSystem": "System",
  "settings.themeLight": "Light",
  "settings.themeDark": "Dark",
  "settings.autoRefresh": "Auto-refresh",
  "settings.autoRefreshDesc": "Periodically check for new emails",
  "settings.refreshInterval": "Refresh interval",
  "settings.preferSSE": "Prefer SSE",
  "settings.preferSSEDesc": "Receive new emails via server-sent events",
  "settings.defaultTab": "Default email view",
  "settings.timeFormat": "Time format",
  "settings.timeRelative": "Relative",
  "settings.timeAbsolute": "Absolute",

  // FAQ
  "faq.title": "FAQ",
  "faq.q1": "What's the difference between public and token inbox?",
  "faq.a1": "A public inbox is accessible without credentials — anyone with the address can view emails. A token inbox requires a mailbox password to issue a token before reading. You can set the access mode per route or per mailbox.",
  "faq.q2": "How do I get a mailbox token?",
  "faq.a2": "Enter the mailbox password on the inbox page to issue a temporary token. You can also call POST /api/v1/token via REST API. Token lifetime is configured on the backend.",
  "faq.q3": "How do I connect a custom domain?",
  "faq.a3": "Add the domain in the tenant console, then configure MX, TXT, SPF and other DNS records as prompted. Once verified, emails to that domain are automatically routed to TabMail. Wildcard and sequence matching are supported.",
  "faq.q4": "How does retention and auto-cleanup work?",
  "faq.a4": "TabMail supports a 4-level retention cascade: plan → tenant override → route override → mailbox override. Expired messages are cleaned up by a background task. You can also manually purge from the inbox.",
  "faq.q5": "What are the Tenant and Admin roles?",
  "faq.a5": "Admin has global control: monitoring, SMTP policy, tenant management, and plan configuration. Tenant manages its own domains, routes, and mailboxes. Regular users access inboxes via mailbox token or public mode.",

  // Inbox enhanced
  "inbox.everyNSec": "Every {sec}s",
  "inbox.paused": "Paused",

  // Auth enhanced
  "auth.copyKey": "Copy key",
  "auth.copyAddr": "Copy address",
  "auth.keyCopied": "Copied",

  // Toast
  "toast.copied": "Address copied",
  "toast.tokenIssued": "Mailbox token issued",
  "toast.authFailed": "Authentication failed",
  "toast.loadFailed": "Failed to load messages",
  "toast.deleteFailed": "Failed to delete",
  "toast.deleted": "Message deleted",
  "toast.allDeleted": "All messages deleted",
  "toast.purgeFailed": "Failed to purge",
  "toast.adminOk": "Admin key configured",
  "toast.apiKeyOk": "API key configured",
};

const allMessages: Record<Locale, Messages> = { zh, en };

interface I18nContextValue {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: (key: string, params?: Record<string, string | number>) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

const storeListeners = new Set<() => void>();

function subscribeLocaleStore(callback: () => void) {
  storeListeners.add(callback);
  return () => { storeListeners.delete(callback); };
}

function getLocaleSnapshot(): Locale {
  if (typeof window === "undefined") return "zh";
  const stored = localStorage.getItem(STORAGE_KEY);
  return (stored === "zh" || stored === "en") ? stored : "zh";
}

function getLocaleServerSnapshot(): Locale {
  return "zh";
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const locale = useSyncExternalStore(
    subscribeLocaleStore,
    getLocaleSnapshot,
    getLocaleServerSnapshot,
  );

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  useEffect(() => {
    storeListeners.forEach((cb) => cb());
  }, []);

  const setLocale = useCallback((l: Locale) => {
    localStorage.setItem(STORAGE_KEY, l);
    document.documentElement.lang = l;
    storeListeners.forEach((cb) => cb());
  }, []);

  const t = useCallback(
    (key: string, params?: Record<string, string | number>): string => {
      const msg = allMessages[locale]?.[key] ?? allMessages.zh[key] ?? key;
      if (!params) return msg;
      return Object.entries(params).reduce(
        (s, [k, v]) => s.replaceAll(`{${k}}`, String(v)),
        msg,
      );
    },
    [locale],
  );

  return (
    <I18nContext value={{ locale, setLocale, t }}>
      {children}
    </I18nContext>
  );
}

export function useI18n() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used within I18nProvider");
  return ctx;
}
