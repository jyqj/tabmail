import type { Locale } from "@/lib/i18n";
const en = {
  title: "Inbound recovery",
  description:
    "Inspect accepted receipts and recover held destinations without replaying completed deliveries.",
  restricted:
    "Platform administrators only. Company administrators and employees cannot inspect cross-company receipts.",
  loading: "Loading current server state…",
  refresh: "Refresh",
  apply: "Apply filters",
  all: "All states",
  state: "Receipt state",
  recipient: "Exact envelope recipient",
  source: "Source",
  anySource: "All sources",
  smtp: "SMTP",
  total: "Matching receipts",
  pageCount: "Receipts on this page",
  processingCount: "Processing on this page",
  heldCount: "Held on this page",
  empty: "No receipts match these filters.",
  sender: "Sender",
  destinations: "Envelope recipients",
  attempts: "Receipt attempts",
  created: "Accepted at",
  inspect: "Inspect",
  previous: "Previous",
  next: "Next",
  page: "Page",
  close: "Close inspection",
  targets: "Mailbox outcomes",
  tenant: "Company ID",
  mailbox: "Frozen mailbox ID",
  address: "Accepted mailbox address",
  error: "Last failure",
  updated: "Snapshot updated at",
  bytes: "Expected original bytes",
  unknown: "Unknown",
  metadataOnly:
    "Byte count is acceptance metadata, not a fresh storage integrity check.",
  legacy_receipt:
    "Legacy receipt: no trustworthy destination ledger. Do not replay automatically; use the documented operator review process.",
  missing_ledger:
    "Managed receipt has no destination ledger. Recovery is blocked; investigate database integrity.",
  not_held:
    "This receipt is not held. Automatic processing, or completion, takes precedence over manual retry.",
  no_held_targets: "No held destinations are eligible for retry.",
  blocked: "The current server snapshot does not permit recovery.",
  reason: "Recovery reason",
  reasonHint:
    "Describe the repaired storage, quota or policy issue. Up to 2,000 Unicode characters. This reason is audited.",
  confirm:
    "I checked the failure and understand that only held destinations will be requeued; original content and mailbox identities stay unchanged.",
  retry: "Requeue held destinations",
  busy: "Submitting review…",
  submitted:
    "Recovery request committed. Reloaded state is authoritative; this is not a delivery confirmation.",
  conflict:
    "The receipt changed since inspection. The latest state has been loaded; review it again before retrying.",
  uncertain:
    "The request could not be confirmed. It was NOT automatically replayed. Inspect the refreshed state before any further action.",
  session:
    "This session expired or no longer has permission. Sign in with a current platform administrator session.",
  loadFailure:
    "Unable to load current recovery state. Cached details and retry controls have been hidden.",
  retention:
    "Held receipts retain original bytes. Monitor storage capacity; this page never deletes originals or reassigns destinations.",
  pending: "Pending",
  processing: "Processing",
  retryState: "Automatic retry",
  done: "Completed",
  dead: "Held for review",
  delivered: "Committed locally",
  held: "Held",
  targetPending: "Not committed",
  lastErrorEmpty: "No recorded failure",
  noTargets: "No destination ledger is available.",
} as const;
const zh: Record<keyof typeof en, string> = {
  title: "入站恢复",
  description:
    "核对已接受邮件的持久化记录，只恢复搁置目标，不重复已经完成的投递。",
  restricted:
    "仅平台运维管理员可用。公司管理员和普通员工不能查看跨公司收件记录。",
  loading: "正在读取服务器当前状态…",
  refresh: "刷新",
  apply: "应用筛选",
  all: "全部状态",
  state: "接收任务状态",
  recipient: "精确匹配信封收件人",
  source: "接收来源",
  anySource: "全部来源",
  smtp: "SMTP",
  total: "匹配任务总数",
  pageCount: "当前页任务",
  processingCount: "当前页处理中",
  heldCount: "当前页待核查",
  empty: "没有匹配这些条件的接收任务。",
  sender: "发件人",
  destinations: "信封收件人",
  attempts: "任务处理次数",
  created: "接受时间",
  inspect: "查看恢复详情",
  previous: "上一页",
  next: "下一页",
  page: "页码",
  close: "关闭详情",
  targets: "逐邮箱处理结果",
  tenant: "公司 ID",
  mailbox: "冻结的目标邮箱 ID",
  address: "接受时的邮箱地址",
  error: "最近失败原因",
  updated: "快照更新时间",
  bytes: "原始邮件预期字节数",
  unknown: "未知",
  metadataOnly: "字节数来自接受时的元数据，不代表刚刚检查过原文是否完整。",
  legacy_receipt:
    "旧任务没有可信的逐邮箱投递账本，不能自动重放；请按运维文档单独核查。",
  missing_ledger:
    "任务标记为持久化恢复，但缺少目标账本；已禁止重试，请核查数据库完整性。",
  not_held: "任务当前并未搁置，请以自动处理或已完成状态为准。",
  no_held_targets: "没有可重试的搁置目标。",
  blocked: "服务器当前快照不允许恢复此任务。",
  reason: "恢复理由",
  reasonHint:
    "说明已经修复的存储、配额或策略问题，最多 2,000 个 Unicode 字符；理由会写入审计。",
  confirm:
    "我已核查失败原因，确认只将搁置目标重新入队；原始内容和目标邮箱身份保持不变。",
  retry: "重新入队搁置目标",
  busy: "正在提交核查结果…",
  submitted:
    "恢复请求已提交，请以重新加载的状态为准；这不表示邮件已经投递完成。",
  conflict: "任务自核查以来已经变化，已重新读取状态；请再次核对后操作。",
  uncertain:
    "无法确认本次请求的结果，系统没有自动重复提交；再次操作前请检查刷新后的状态。",
  session: "会话已过期或权限已撤销，请使用有效的平台管理员账号重新登录。",
  loadFailure: "无法获取当前恢复状态；旧详情和重试入口已隐藏。",
  retention:
    "搁置任务会保留原始邮件，请监控存储容量。本页不会删除原文，也不会更换收件目标。",
  pending: "待处理",
  processing: "处理中",
  retryState: "等待自动重试",
  done: "全部完成",
  dead: "搁置待核查",
  delivered: "已完成本地入库",
  held: "已搁置",
  targetPending: "尚未入库",
  lastErrorEmpty: "无失败记录",
  noTargets: "没有可读取的目标账本。",
};
export function ingressCopy(locale: Locale) {
  return locale === "zh" ? zh : en;
}
export type IngressCopy = ReturnType<typeof ingressCopy>;
export function receiptStateLabel(state: string, copy: IngressCopy) {
  return (
    (
      {
        pending: copy.pending,
        processing: copy.processing,
        retry: copy.retryState,
        done: copy.done,
        dead: copy.dead,
      } as Record<string, string>
    )[state] ?? state
  );
}
export function localTime(value: string, locale: Locale) {
  const time = new Date(value);
  return Number.isFinite(time.getTime())
    ? time.toLocaleString(locale === "zh" ? "zh-CN" : "en-US")
    : "—";
}
