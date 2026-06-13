export const API_KEY_SCOPE_OPTIONS = [
  { value: "domains:read", label: "Domains read" },
  { value: "domains:write", label: "Domains write" },
  { value: "routes:read", label: "Routes read" },
  { value: "routes:write", label: "Routes write" },
  { value: "mailboxes:read", label: "Mailboxes read" },
  { value: "mailboxes:write", label: "Mailboxes write" },
  { value: "messages:read", label: "Messages read" },
  { value: "messages:write", label: "Messages write" },
  { value: "send:read", label: "Send read" },
  { value: "send:write", label: "Send write" },
  { value: "webhooks:read", label: "Webhooks read" },
  { value: "webhooks:write", label: "Webhooks write" },
] as const;

export const DEFAULT_API_KEY_SCOPES = [
  "domains:read",
  "routes:read",
  "mailboxes:read",
  "messages:read",
];

export const ALL_API_KEY_SCOPES = API_KEY_SCOPE_OPTIONS.map((scope) => scope.value);

export interface APIKeyTemplate {
  id: string;
  scopes: string[];
}

export const API_KEY_TEMPLATES: APIKeyTemplate[] = [
  { id: "readonly", scopes: ["domains:read", "routes:read", "mailboxes:read", "messages:read", "send:read"] },
  { id: "inbox", scopes: ["mailboxes:read", "messages:read"] },
  { id: "send", scopes: ["send:read", "send:write"] },
  { id: "domain", scopes: ["domains:read", "domains:write", "routes:read", "routes:write"] },
  { id: "webhook", scopes: ["webhooks:read", "webhooks:write"] },
  { id: "custom", scopes: [] },
];
