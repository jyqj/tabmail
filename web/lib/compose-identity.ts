import type { WorkMailbox } from "./company";

// Interaction only: the API re-authorizes the chosen identity and any template.
// Prefer the current sendable mailbox, then a free-form sender, then a
// template-only sender. The same decision powers button gates and execution.
export function composeIdentity(
  mailboxes: WorkMailbox[],
  preferred?: WorkMailbox,
): WorkMailbox | undefined {
  const current = mailboxes.find((box) => box.mailbox.id === preferred?.mailbox.id);
  return (current?.can_send ? current : undefined)
    ?? mailboxes.find((box) => box.can_send && !box.template_only)
    ?? mailboxes.find((box) => box.can_send);
}
