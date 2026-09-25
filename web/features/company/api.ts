import { company, type MailSendPolicy } from "@/lib/company";
export interface OffboardingOptions {
    drafts: "seal" | "transfer_owned" | "discard";
}
export interface OffboardingImpact {
    mailboxes: number;
    drafts: number;
    transferable_drafts: number;
    attachments: number;
    api_keys: number;
    grants: number;
    queued: number;
    in_flight: number;
    uncertain: number;
}
export interface OffboardingPlan {
    id: string;
    target_id: string;
    successor_id: string;
    options: OffboardingOptions;
    impact: OffboardingImpact;
    reason: string;
    state: "preview" | "executed";
    expires_at: string;
    executed_at?: string;
}
export interface CompanyOverview {
    mailboxes: number;
    active_employees: number;
    pending_invitations: number;
    queued: number;
    uncertain: number;
    index_failed: number;
}
export interface CompanyAdminAudit {
    id: string;
    actor: string;
    action: string;
    resource_type: string;
    resource_id?: string;
    reason?: string;
    created_at: string;
}
export interface AccessExplanation {
    mailbox_id: string;
    user_id: string;
    source: "owner" | "grant" | "none";
    active: boolean;
    can_read: boolean;
    can_organize: boolean;
    can_send: boolean;
    template_only: boolean;
    send_policy: MailSendPolicy;
    reasons: string[];
}
export const previewOffboarding = (target: string, successor: string, options: OffboardingOptions, reason: string) => company<OffboardingPlan>(`/employees/${encodeURIComponent(target)}/offboard/preview`, { method: "POST", body: { successor_user_id: successor, options, reason } });
export const executeOffboarding = (target: string, plan: string) => company<OffboardingPlan>(`/employees/${encodeURIComponent(target)}/offboard`, { method: "POST", body: { plan_id: plan } });
