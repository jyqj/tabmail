import type { Submission } from "@/lib/company";
import { useText } from "@/components/company/common";
const STATUS_LABELS: Record<Submission["status"], {
    zh: string;
    en: string;
    className: string;
}> = {
    cancelled: { zh: "已取消未完成投递", en: "Unfinished delivery cancelled", className: "bg-muted text-muted-foreground" },
    submitted: {
        zh: "已入队",
        en: "Submitted",
        className: "bg-muted text-foreground",
    },
    waiting: {
        zh: "等待中",
        en: "Waiting",
        className: "bg-muted text-foreground",
    },
    sending: {
        zh: "发送中",
        en: "Sending",
        className: "bg-muted text-foreground",
    },
    partially_accepted: {
        zh: "部分已接受",
        en: "Partially accepted",
        className: "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200",
    },
    accepted: {
        zh: "下一跳已接受",
        en: "Accepted by next hop",
        className: "bg-green-100 text-green-900 dark:bg-green-950 dark:text-green-200",
    },
    needs_attention: {
        zh: "需要处理",
        en: "Needs attention",
        className: "bg-destructive text-destructive-foreground",
    },
};
export function StatusBadge({ status }: {
    status: Submission["status"];
}) {
    const t = useText();
    const s = STATUS_LABELS[status] ?? STATUS_LABELS.submitted;
    return (<span className={`rounded px-1.5 py-0.5 text-xs ${s.className}`}>
      {t(s.zh, s.en)}
    </span>);
}
