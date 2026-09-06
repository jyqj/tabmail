import { describe, it, expect } from "vitest";
import { composePayload, type CompanyCompose } from "./company";
const form: CompanyCompose = {
  from: "alice@company.test",
  recipients: "a@example.test, b@example.test\nc@example.test",
  templateId: "",
  variables: {},
  subject: "Hello",
  text: "Body",
  templateRequired: false,
};
describe("company compose", () => {
  it("keeps raw and template sending mutually exclusive", () => {
    expect(composePayload(form)).toEqual({
      from: form.from,
      to: ["a@example.test", "b@example.test", "c@example.test"],
      subject: "Hello",
      text_body: "Body",
    });
    const payload = composePayload({
      ...form,
      templateId: "version-id",
      variables: { customer: "Alice" },
    });
    expect(payload).toEqual({
      from: form.from,
      to: ["a@example.test", "b@example.test", "c@example.test"],
      template_id: "version-id",
      variables: { customer: "Alice" },
    });
    expect(payload).not.toHaveProperty("subject");
    expect(payload).not.toHaveProperty("text_body");
  });
  it("does not allow restricted raw sends", () =>
    expect(() => composePayload({ ...form, templateRequired: true })).toThrow(
      "必须使用",
    ));
  it("requires sender, recipients, and nonempty free-form content", () => {
    for (const partial of [
      { from: "" },
      { recipients: " , ; \n" },
      { subject: " " },
      { text: "" },
    ])
      expect(() => composePayload({ ...form, ...partial })).toThrow();
  });
});
