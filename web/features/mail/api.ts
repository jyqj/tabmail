import { request } from "@/lib/api/base";
import { company, workPath, type MailDraft } from "@/lib/company";
import type { APIListResponse } from "@/lib/types";
export interface ArchivedMail {
    id: string;
    mailbox_id: string;
    from: string;
    to: string[];
    subject: string;
    created_at: string;
    attachment_count: number;
    revision: number;
    delivery_available: boolean;
}
export interface ContentIndexStatus {
    total: number;
    indexed: number;
    failed: number;
}
export const draftPage = (page: number) => request<APIListResponse<MailDraft>>("/api/v1/company/drafts", { params: { page, per_page: 30 } });
export const archivedPage = (mailbox: string, folder: string, q: string, page: number) => request<APIListResponse<ArchivedMail>>(`/api/v1/company${workPath(mailbox)}/sent`, { params: { folder, q, page, per_page: 30 } });
export const indexStatus = (mailbox: string) => company<ContentIndexStatus>(`${workPath(mailbox)}/index-status`);
export const changeArchived = (mailbox: string, id: string, revision: number, action: string) => company(`${workPath(mailbox)}/sent/${encodeURIComponent(id)}/actions`, { method: "POST", body: { revision, action } });
