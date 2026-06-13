export { getBaseUrl } from "./api/base";
export {
  createAPIKey,
  createPlan,
  createTenant,
  createUserAPIKey,
  deletePlan,
  deleteTenant,
  deleteUser,
  getSMTPPolicy,
  getStats,
  getTenantConfig,
  inviteAdmin,
  listIngestJobs,
  listAPIKeys,
  listAudit,
  listMonitorHistory,
  listPlans,
  listSettings,
  listTenants,
  listUsers,
  listUserAPIKeys,
  listWebhookDeliveries,
  revokeAPIKey,
  revokeUserAPIKey,
  streamAdminMonitorEvents,
  updatePlan,
  updateSettings,
  updateSMTPPolicy,
  updateTenantOverrides,
  updateUser,
} from "./api/admin";
export {
  acceptInvite,
  issueToken,
  login,
  register,
  logoutSession,
  changePassword,
} from "./api/auth";
export {
  createDomain,
  createRoute,
  deleteDomain,
  listAdminDomains,
  deleteRoute,
  explainRoute,
  getVerificationStatus,
  listDomains,
  listOpenDomains,
  listRoutes,
  suggestAddress,
  suggestOpenAddress,
  updateAdminDomainAccess,
  verifyDomain,
} from "./api/domains";
export type { RouteExplainResult } from "./api/domains";
export {
  createSendIdentity,
  deleteSendIdentity,
  listSendIdentities,
} from "./api/send-identities";
export { createMailbox, deleteMailbox, listMailboxes } from "./api/mailboxes";
export {
  breakGlassRead,
  breakGlassSource,
  deleteMessage,
  getMessage,
  getMessageSource,
  listMessages,
  markMessageSeen,
  purgeMailbox,
  streamMailboxEvents,
} from "./api/messages";
export {
  getOutboundJob,
  listOutboundJobs,
  sendEmail,
} from "./api/outbound";
export {
  createPermissionProfile,
  deletePermissionProfile,
  deleteUserPermissionOverride,
  getMyPermissions,
  getUserPermission,
  listPermissionProfiles,
  setUserPermissionOverride,
  updatePermissionProfile,
} from "./api/permissions";
export { healthCheck } from "./api/system";
export {
  createWebhookEndpoint,
  deleteWebhookEndpoint,
  listWebhookEndpoints,
  updateWebhookEndpoint,
} from "./api/webhook-endpoints";
export type { WebhookEndpoint } from "./api/webhook-endpoints";
