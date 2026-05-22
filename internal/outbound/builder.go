package outbound

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"regexp"
	"strings"
	"time"

	"tabmail/internal/models"
)

var validHeaderName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// isValidHeaderName checks that a header name contains only safe characters
// (alphanumeric and hyphens, starting with alphanumeric, max 126 chars).
func isValidHeaderName(name string) bool {
	return len(name) <= 126 && validHeaderName.MatchString(name)
}

// forbiddenCustomHeaders are headers that must not be set via user-supplied custom headers.
var forbiddenCustomHeaders = map[string]struct{}{
	"from":                      {},
	"to":                        {},
	"cc":                        {},
	"bcc":                       {},
	"subject":                   {},
	"date":                      {},
	"message-id":                {},
	"mime-version":              {},
	"content-type":              {},
	"content-transfer-encoding": {},
	"return-path":               {},
	"sender":                    {},
	"received":                  {},
	"dkim-signature":            {},
	"domainkey-signature":       {},
	"arc-seal":                  {},
	"arc-message-signature":     {},
	"arc-authentication-results": {},
	"x-mailer":                  {},
	"x-originating-ip":          {},
	"x-originating-email":       {},
	"x-google-dkim-signature":   {},
	"authentication-results":    {},
	"received-spf":              {},
}

// BuildMIME constructs a complete MIME message from an OutboundJob.
// All outbound mail goes through the structured builder to enforce header
// safety checks (CRLF sanitization, forbidden-header blocking, BCC omission).
// The RawMIME passthrough was removed because it bypassed every safety measure;
// if a future code path needs raw MIME support, it must add validation here.
func BuildMIME(job *models.OutboundJob) ([]byte, error) {
	var buf bytes.Buffer

	writeHeader(&buf, "From", job.MailFrom)

	// Build To and CC from the SendRequest's structured fields.
	// job.RcptTo contains all recipients (To+CC+BCC) for SMTP envelope,
	// but MIME headers must only include To and CC — BCC must be omitted.
	toAddrs, ccAddrs := splitRecipientHeaders(job)
	if len(toAddrs) > 0 {
		writeHeader(&buf, "To", strings.Join(toAddrs, ", "))
	}
	if len(ccAddrs) > 0 {
		writeHeader(&buf, "Cc", strings.Join(ccAddrs, ", "))
	}

	writeHeader(&buf, "Subject", sanitizeHeaderValue(job.Subject))
	writeHeader(&buf, "Date", time.Now().UTC().Format(time.RFC1123Z))
	writeHeader(&buf, "Message-ID", job.MessageIDHeader)
	writeHeader(&buf, "MIME-Version", "1.0")

	if len(job.HeadersJSON) > 0 {
		var headers map[string]any
		if err := json.Unmarshal(job.HeadersJSON, &headers); err == nil {
			for k, raw := range headers {
				v, ok := raw.(string)
				if !ok {
					continue // Skip non-string values (e.g. _to, _cc arrays)
				}
				if _, blocked := forbiddenCustomHeaders[strings.ToLower(k)]; blocked {
					continue
				}
				if strings.HasPrefix(k, "_") {
					continue // Skip internal metadata keys (_to, _cc, etc.)
				}
				if !isValidHeaderName(k) {
					continue // Skip invalid header names to prevent injection
				}
				writeHeader(&buf, k, sanitizeHeaderValue(v))
			}
		}
	}

	hasText := job.TextBody != ""
	hasHTML := job.HTMLBody != ""

	if hasText && hasHTML {
		w := multipart.NewWriter(&buf)
		writeHeader(&buf, "Content-Type", fmt.Sprintf("multipart/alternative; boundary=%s", w.Boundary()))
		buf.WriteString("\r\n")

		textPart, _ := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/plain; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		qpw := quotedprintable.NewWriter(textPart)
		qpw.Write([]byte(job.TextBody))
		qpw.Close()

		htmlPart, _ := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/html; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		qpw = quotedprintable.NewWriter(htmlPart)
		qpw.Write([]byte(job.HTMLBody))
		qpw.Close()

		w.Close()
	} else if hasHTML {
		writeHeader(&buf, "Content-Type", "text/html; charset=utf-8")
		writeHeader(&buf, "Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		qpw := quotedprintable.NewWriter(&buf)
		qpw.Write([]byte(job.HTMLBody))
		qpw.Close()
	} else {
		writeHeader(&buf, "Content-Type", "text/plain; charset=utf-8")
		writeHeader(&buf, "Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		qpw := quotedprintable.NewWriter(&buf)
		qpw.Write([]byte(job.TextBody))
		qpw.Close()
	}

	return buf.Bytes(), nil
}

// splitRecipientHeaders extracts To and CC addresses from the job's HeadersJSON.
// BCC addresses are deliberately excluded from MIME headers.
func splitRecipientHeaders(job *models.OutboundJob) (to, cc []string) {
	if len(job.HeadersJSON) > 0 {
		var h map[string]any
		if err := json.Unmarshal(job.HeadersJSON, &h); err == nil {
			if ccVal, ok := h["_cc"]; ok {
				if arr, ok := ccVal.([]any); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							cc = append(cc, s)
						}
					}
				}
			}
		}
	}

	// To addresses = RcptTo minus CC and BCC.
	// We reconstruct To by excluding known CC addresses from all recipients.
	// Since BCC is not stored separately after Submit, we use a heuristic:
	// everything in RcptTo that is not in _cc is either To or BCC.
	// Without separate BCC tracking, we must accept that BCC recipients appear
	// nowhere in headers (which is correct behavior).

	// If _to is stored in headers, use it directly.
	if len(job.HeadersJSON) > 0 {
		var h map[string]any
		if err := json.Unmarshal(job.HeadersJSON, &h); err == nil {
			if toVal, ok := h["_to"]; ok {
				if arr, ok := toVal.([]any); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							to = append(to, s)
						}
					}
				}
				return to, cc
			}
		}
	}

	// Fallback: if no _to/_cc metadata, put all RcptTo into To (legacy behavior).
	return job.RcptTo, nil
}

// sanitizeHeaderValue removes CR and LF characters to prevent header injection.
func sanitizeHeaderValue(v string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(v)
}

func writeHeader(buf *bytes.Buffer, key, value string) {
	io.WriteString(buf, key)
	io.WriteString(buf, ": ")
	io.WriteString(buf, sanitizeHeaderValue(value))
	io.WriteString(buf, "\r\n")
}
