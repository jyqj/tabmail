package resolver

// ReasonCode is a standard code explaining why an address was accepted or rejected.
type ReasonCode string

const (
	ReasonRouteMatched         ReasonCode = "ROUTE_MATCHED"
	ReasonDomainNotFound       ReasonCode = "DOMAIN_NOT_FOUND"
	ReasonDomainNotVerified    ReasonCode = "DOMAIN_NOT_VERIFIED"
	ReasonDomainMXNotVerified  ReasonCode = "DOMAIN_MX_NOT_VERIFIED"
	ReasonRouteNotFound        ReasonCode = "ROUTE_NOT_FOUND"
	ReasonMailboxNotFound      ReasonCode = "MAILBOX_NOT_FOUND"
	ReasonMailboxExpired       ReasonCode = "MAILBOX_EXPIRED"
	ReasonMailboxQuotaExceeded ReasonCode = "MAILBOX_QUOTA_EXCEEDED"
	ReasonTenantQuotaExceeded  ReasonCode = "TENANT_QUOTA_EXCEEDED"
	ReasonTenantSuspended      ReasonCode = "TENANT_SUSPENDED"
	ReasonPolicyRejected       ReasonCode = "POLICY_REJECTED"
	ReasonRateLimited          ReasonCode = "RATE_LIMITED"
	ReasonMessageTooLarge      ReasonCode = "MESSAGE_TOO_LARGE"
	ReasonAuthRequired         ReasonCode = "AUTH_REQUIRED"
	ReasonZoneNotAllowed       ReasonCode = "ZONE_NOT_ALLOWED"
	ReasonGrantRequired        ReasonCode = "GRANT_REQUIRED"
	ReasonSendAsRequired       ReasonCode = "SEND_AS_REQUIRED"
	ReasonSendQuotaExceeded    ReasonCode = "SEND_QUOTA_EXCEEDED"
	ReasonMailboxWouldCreate   ReasonCode = "MAILBOX_WOULD_CREATE"
)

// ExplainResult describes the outcome of a route explain request.
type ExplainResult struct {
	Accepted           bool       `json:"accepted"`
	TenantID           string     `json:"tenant_id,omitempty"`
	ZoneID             string     `json:"zone_id,omitempty"`
	ZoneDomain         string     `json:"zone_domain,omitempty"`
	RouteID            string     `json:"route_id,omitempty"`
	RouteType          string     `json:"route_type,omitempty"`
	MailboxID          string     `json:"mailbox_id,omitempty"`
	MailboxAddress     string     `json:"mailbox_address,omitempty"`
	AutoCreateMailbox  bool       `json:"auto_create_mailbox"`
	WouldCreateMailbox bool       `json:"would_create_mailbox"`
	ReasonCode         ReasonCode `json:"reason_code"`
	Steps              []string   `json:"steps"`
}
