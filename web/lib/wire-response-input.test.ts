import { describe, expect, expectTypeOf, it, vi } from "vitest";
import type {
  CompanySettings, CompanySettingsInput, MailDraft, MailDraftInput,
  MailTemplate, MailTemplateInput, NewMailDraft, WorkGrant, WorkGrantInput,
} from "./company";
import type {
  IngestJob, Mailbox, OutboundJob, SMTPPolicy, SMTPPolicyInput,
  TenantOverride, TenantOverrideInput, WebhookDelivery,
} from "./types";
import { DraftWriter } from "@/features/mail/draft-writer";

// Required properties already satisfy the required form of their own picked shape.
type RequiredKeys<T> = { [K in keyof T]-?: Pick<T, K> extends Required<Pick<T, K>> ? K : never }[keyof T];

describe("wire response and editable input boundaries", () => {
  it("requires all 21 server-present fields without making input metadata mandatory", () => {
    expectTypeOf<Extract<RequiredKeys<WorkGrant>, "tenant_id" | "mailbox_id" | "created_at" | "updated_at">>()
      .toEqualTypeOf<"tenant_id" | "mailbox_id" | "created_at" | "updated_at">();
    expectTypeOf<Extract<RequiredKeys<CompanySettings>, "tenant_id" | "domain">>()
      .toEqualTypeOf<"tenant_id" | "domain">();
    expectTypeOf<Extract<RequiredKeys<MailTemplate>, "id" | "retired" | "updated_at">>()
      .toEqualTypeOf<"id" | "retired" | "updated_at">();
    expectTypeOf<Extract<RequiredKeys<MailDraft>, "id" | "updated_at">>()
      .toEqualTypeOf<"id" | "updated_at">();
    expectTypeOf<Extract<RequiredKeys<TenantOverride>, "id" | "tenant_id" | "updated_at">>()
      .toEqualTypeOf<"id" | "tenant_id" | "updated_at">();
    expectTypeOf<Extract<RequiredKeys<Mailbox>, "kind">>().toEqualTypeOf<"kind">();
    expectTypeOf<Extract<RequiredKeys<SMTPPolicy>, "updated_at">>().toEqualTypeOf<"updated_at">();
    expectTypeOf<Extract<RequiredKeys<WebhookDelivery>, "payload" | "last_error">>()
      .toEqualTypeOf<"payload" | "last_error">();
    expectTypeOf<Extract<RequiredKeys<IngestJob>, "last_error">>().toEqualTypeOf<"last_error">();
    expectTypeOf<Extract<RequiredKeys<OutboundJob>, "content_redacted" | "delivery_uncertain">>()
      .toEqualTypeOf<"content_redacted" | "delivery_uncertain">();
    expectTypeOf<Pick<WebhookDelivery, "payload">>().toMatchTypeOf<{ payload: unknown }>();
    const nullablePayload: Pick<WebhookDelivery, "payload"> = { payload: null };
    expect(nullablePayload).toHaveProperty("payload", null);

    const settings: CompanySettingsInput = { name: "Example", primary_zone_id: "zone", revision: 0 };
    const grant: WorkGrantInput = { user_id: "user", can_read: true, can_send: false, can_organize: false, template_only: false };
    const template: MailTemplateInput = { name: "Greeting", revision: 0, draft: { subject: "Hello", text_body: "Hi", html_body: "", variables: [] } };
    const override: TenantOverrideInput = { max_domains: null };
    const policy: SMTPPolicyInput = { default_accept: true, default_store: true, accept_domains: [], reject_domains: [], store_domains: [], discard_domains: [], reject_origin_domains: [] };
    for (const input of [settings, grant, template, override, policy]) {
      expect(input).not.toHaveProperty("id");
      expect(input).not.toHaveProperty("tenant_id");
      expect(input).not.toHaveProperty("updated_at");
    }
  });

  it("keeps a new draft timestamp-free until an actual acknowledgement and owns its no-op response", async () => {
    const seed: NewMailDraft = { mailbox_id: "mailbox", revision: 0, payload: { to: ["recipient@example.test"], subject: "Hello", text_body: "Body" } };
    let acknowledged: MailDraft | undefined;
    const write = vi.fn(async (command: MailDraftInput): Promise<MailDraft> => {
      expect(command.id).toMatch(/^[0-9a-f-]{36}$/);
      expect(command).not.toHaveProperty("updated_at");
      acknowledged = {
        ...structuredClone(command), id: command.id!, revision: 1,
        updated_at: "2026-10-02T12:00:00Z",
      };
      return acknowledged;
    });
    const lane = new DraftWriter(seed, { write, read: vi.fn() });
    const saved = await lane.save(seed);
    expect(write).toHaveBeenCalledWith(expect.objectContaining({ id: lane.id, revision: 0 }), true);
    expect(saved.updated_at).toBe("2026-10-02T12:00:00Z");
    expect(seed).not.toHaveProperty("id");
    expect(seed).not.toHaveProperty("updated_at");
    acknowledged!.payload.subject = "borrowed transport mutation";
    saved.payload.to.push("caller-mutation@example.test");
    const unchanged = await lane.save(seed);
    expect(unchanged.payload).toEqual(seed.payload);
    expect(unchanged.updated_at).toBe("2026-10-02T12:00:00Z");
    unchanged.payload.subject = "no-op caller mutation";
    expect((await lane.save(seed)).payload.subject).toBe("Hello");
    expect(write).toHaveBeenCalledTimes(1);
  });
});
