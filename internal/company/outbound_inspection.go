package company

import (
	"context"
	"encoding/json"
	"net/textproto"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
)

// OutboundRecoveryInspector is an audited platform-operations capability.
// Neither receipt visibility nor ordinary mailbox read grants this port.
// An error must return nil: no uncommitted inspection may escape.
type OutboundRecoveryInspector interface {
	InspectOutboundRecovery(context.Context, authz.Actor, uuid.UUID, string) (*OutboundInspection, error)
}

// OutboundInspection deliberately cannot embed OutboundJob or Recipient:
// adding fields to storage models must never extend the disclosure boundary.
type OutboundInspection struct {
	Job        OutboundInspectionJob         `json:"job"`
	Recipients []OutboundInspectionRecipient `json:"recipients"`
}
type OutboundInspectionJob struct {
	ID        uuid.UUID            `json:"id"`
	TenantID  uuid.UUID            `json:"tenant_id"`
	State     models.OutboundState `json:"state"`
	Status    string               `json:"status"` // accepted means next-hop acceptance, not delivery.
	CreatedAt time.Time            `json:"created_at"`
	UpdatedAt time.Time            `json:"updated_at"`
	MailFrom  string               `json:"mail_from"`
	To        []string             `json:"to"`
	CC        []string             `json:"cc"`
	BCC       []string             `json:"bcc"` // explicit structured recipients, never a wire header.
	Subject   string               `json:"subject"`
	TextBody  string               `json:"text_body"`
	HTMLBody  string               `json:"html_body"`
	Headers   map[string]string    `json:"headers"`
}
type OutboundInspectionRecipient struct {
	Address         string    `json:"address"`
	Kind            string    `json:"kind"`
	State           string    `json:"state"`
	Attempts        int       `json:"attempts"`
	SMTPCode        int       `json:"smtp_code"`
	EnhancedCode    string    `json:"enhanced_code,omitempty"`
	DiagnosticClass string    `json:"diagnostic_class"`
	UpdatedAt       time.Time `json:"updated_at"`
}

var inspectionEnhancedCode = regexp.MustCompile(`(?:^|[[:space:]])([245]\.[0-9]{1,3}\.[0-9]{1,3})(?:[[:space:]]|$)`)

// ProjectOutboundInspection is pure projection, not an authorization decision.
// Callers supply the complete ledger from their fenced transaction. It never
// projects raw MIME, diagnostics, storage keys, leases or credential fields.
func ProjectOutboundInspection(j *models.OutboundJob, ledger []Recipient) *OutboundInspection {
	if j == nil {
		return nil
	}
	states := make([]string, 0, len(ledger)+1)
	expected := map[string]bool{}
	for _, address := range j.RcptTo {
		expected[address] = true
	}
	seen := map[string]bool{}
	complete := j.RecipientLedger && len(expected) > 0 && len(expected) == len(ledger)
	for _, r := range ledger {
		states = append(states, r.State)
		if !expected[r.Address] || seen[r.Address] {
			complete = false
		}
		seen[r.Address] = true
	}
	if !complete || (j.InFlightDomain != "" && j.State != models.OutboundProcessing) {
		states = append(states, delivery.Uncertain)
	}
	out := &OutboundInspection{Job: OutboundInspectionJob{ID: j.ID, TenantID: j.TenantID, State: j.State, Status: delivery.DeriveSubmissionStatus(j.State, states), CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, MailFrom: j.MailFrom, To: append([]string{}, j.To...), CC: append([]string{}, j.CC...), BCC: append([]string{}, j.BCC...), Subject: j.Subject, TextBody: j.TextBody, HTMLBody: j.HTMLBody, Headers: inspectionHeaders(j.HeadersJSON)}, Recipients: make([]OutboundInspectionRecipient, 0, len(ledger))}
	categories := map[string]string{}
	for _, s := range j.To {
		categories[s] = "to"
	}
	for _, s := range j.CC {
		categories[s] = "cc"
	}
	for _, s := range j.BCC {
		categories[s] = "bcc"
	}
	for _, r := range ledger {
		kind := categories[r.Address]
		if kind == "" {
			kind = "envelope"
		}
		state := r.State
		switch state {
		case delivery.Pending, delivery.Accepted, delivery.Temporary, delivery.Permanent, delivery.Uncertain:
		default:
			state = "unknown"
		}
		code := r.SMTPCode
		if code < 200 || code > 599 {
			code = 0
		}
		enhanced := ""
		if match := inspectionEnhancedCode.FindStringSubmatch(r.Diagnostic); len(match) == 2 {
			enhanced = match[1]
		}
		out.Recipients = append(out.Recipients, OutboundInspectionRecipient{Address: r.Address, Kind: kind, State: state, Attempts: r.Attempts, SMTPCode: code, EnhancedCode: enhanced, DiagnosticClass: state, UpdatedAt: r.UpdatedAt})
	}
	return out
}
func inspectionHeaders(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	var headers map[string]string
	if json.Unmarshal(raw, &headers) != nil {
		return out
	}
	// No general X-*, auth, DKIM, Received or Bcc escape hatch. Reject CR/LF
	// rather than preserving injected headers inside an otherwise allowed key.
	for name, value := range headers {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		switch canonical {
		case "From", "To", "Cc", "Subject", "Date", "Message-Id", "Reply-To", "In-Reply-To", "References", "Mime-Version", "Content-Type", "Content-Transfer-Encoding", "Content-Disposition", "Content-Id":
		default:
			continue
		}
		if strings.ContainsAny(value, "\r\n") {
			continue
		}
		out[canonical] = value
	}
	return out
}
