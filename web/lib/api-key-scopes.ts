export const API_KEY_SCOPE_OPTIONS = [
  { value: "domains:read", label: "Domains read" },
  { value: "domains:write", label: "Domains write" },
  { value: "routes:read", label: "Routes read" },
  { value: "routes:write", label: "Routes write" },
  { value: "mailboxes:read", label: "Mailboxes read" },
  { value: "mailboxes:write", label: "Mailboxes write" },
  { value: "messages:read", label: "Messages read" },
  { value: "messages:write", label: "Messages write" },
] as const;

export const DEFAULT_API_KEY_SCOPES = [
  "domains:read",
  "routes:read",
  "mailboxes:read",
  "messages:read",
];

export const ALL_API_KEY_SCOPES = API_KEY_SCOPE_OPTIONS.map((scope) => scope.value);
