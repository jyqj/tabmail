export { getBaseUrl } from "./api/base";
export {
  createAPIKey,
  createPlan,
  createTenant,
  deletePlan,
  deleteTenant,
  getSMTPPolicy,
  getStats,
  getTenantConfig,
  listAPIKeys,
  listAudit,
  listMonitorHistory,
  listPlans,
  listTenants,
  revokeAPIKey,
  streamAdminMonitorEvents,
  updatePlan,
  updateSMTPPolicy,
  updateTenantOverrides,
} from "./api/admin";
export { issueToken } from "./api/auth";
export {
  createDomain,
  createRoute,
  deleteDomain,
  deleteRoute,
  getVerificationStatus,
  listDomains,
  listRoutes,
  verifyDomain,
} from "./api/domains";
export { createMailbox, deleteMailbox, listMailboxes } from "./api/mailboxes";
export {
  deleteMessage,
  getMessage,
  getMessageSource,
  listMessages,
  markMessageSeen,
  purgeMailbox,
  streamMailboxEvents,
} from "./api/messages";
export { healthCheck } from "./api/system";
