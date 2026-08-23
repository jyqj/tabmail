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
	"from":                       {},
	"to":                         {},
	"cc":                         {},
	"bcc":                        {},
	"subject":                    {},
	"date":                       {},
	"message-id":                 {},
	"mime-version":               {},
	"content-type":               {},
	"content-transfer-encoding":  {},
	"return-path":                {},
	"sender":                     {},
	"received":                   {},
	"dkim-signature":             {},
	"domainkey-signature":        {},
	"arc-seal":                   {},
	"arc-message-signature":      {},
	"arc-authentication-results": {},
	"x-mailer":                   {},
	"x-originating-ip":           {},
	"x-originating-email":        {},
	"x-google-dkim-signature":    {},
	"authentication-results":     {},
	"received-spf":               {},
}

// Message is the semantic input to the MIME builder. Recipients are supplied by
// role: To and CC become headers, while BCC is envelope-only and is NEVER
// emitted as a header. This module is the single home for outbound message
// safety — it owns header-name validation, forbidden-header blocking, CRLF
// stripping, and body encoding — so a caller holding only this interface cannot
// construct an unsafe or BCC-leaking message. The persistence row carries
// recipients structurally (To/CC/BCC), so the builder never reverse-engineers
// them from a header blob.
type Message struct {
	From      string
	To        []string
	CC        []string
	BCC       []string
	Subject   string
	TextBody  string
	HTMLBody  string
	Headers   map[string]string
	MessageID string
}

// EnvelopeRecipients returns the full RCPT TO set (To + CC + BCC) used for the
// SMTP envelope. BCC appears here but never in a rendered header.
func (m Message) EnvelopeRecipients() []string {
	rcpt := make([]string, 0, len(m.To)+len(m.CC)+len(m.BCC))
	rcpt = append(rcpt, m.To...)
	rcpt = append(rcpt, m.CC...)
	rcpt = append(rcpt, m.BCC...)
	return rcpt
}

// Build renders the message to a complete MIME wire form. It returns an error if
// the body cannot be encoded, rather than silently emitting a truncated message.
func Build(m Message) ([]byte, error) {
	var buf bytes.Buffer

	writeHeader(&buf, "From", m.From)
	if len(m.To) > 0 {
		writeHeader(&buf, "To", strings.Join(m.To, ", "))
	}
	if len(m.CC) > 0 {
		writeHeader(&buf, "Cc", strings.Join(m.CC, ", "))
	}
	writeHeader(&buf, "Subject", m.Subject)
	writeHeader(&buf, "Date", time.Now().UTC().Format(time.RFC1123Z))
	writeHeader(&buf, "Message-ID", m.MessageID)
	writeHeader(&buf, "MIME-Version", "1.0")

	for k, v := range m.Headers {
		if _, blocked := forbiddenCustomHeaders[strings.ToLower(k)]; blocked {
			continue
		}
		if !isValidHeaderName(k) {
			continue // Skip invalid header names to prevent injection.
		}
		writeHeader(&buf, k, v)
	}

	if err := writeBody(&buf, m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeBody renders the text and/or HTML body, surfacing any encoding error.
func writeBody(buf *bytes.Buffer, m Message) error {
	hasText := m.TextBody != ""
	hasHTML := m.HTMLBody != ""

	switch {
	case hasText && hasHTML:
		w := multipart.NewWriter(buf)
		writeHeader(buf, "Content-Type", fmt.Sprintf("multipart/alternative; boundary=%s", w.Boundary()))
		buf.WriteString("\r\n")
		if err := writeQPPart(w, "text/plain; charset=utf-8", m.TextBody); err != nil {
			return err
		}
		if err := writeQPPart(w, "text/html; charset=utf-8", m.HTMLBody); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("close multipart body: %w", err)
		}
	case hasHTML:
		return writeSinglePartQP(buf, "text/html; charset=utf-8", m.HTMLBody)
	default:
		return writeSinglePartQP(buf, "text/plain; charset=utf-8", m.TextBody)
	}
	return nil
}

func writeQPPart(w *multipart.Writer, contentType, body string) error {
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return fmt.Errorf("create mime part: %w", err)
	}
	return encodeQP(part, body)
}

func writeSinglePartQP(buf *bytes.Buffer, contentType, body string) error {
	writeHeader(buf, "Content-Type", contentType)
	writeHeader(buf, "Content-Transfer-Encoding", "quoted-printable")
	buf.WriteString("\r\n")
	return encodeQP(buf, body)
}

func encodeQP(w io.Writer, body string) error {
	qpw := quotedprintable.NewWriter(w)
	if _, err := qpw.Write([]byte(body)); err != nil {
		return fmt.Errorf("encode body: %w", err)
	}
	if err := qpw.Close(); err != nil {
		return fmt.Errorf("flush body: %w", err)
	}
	return nil
}

// messageFromJob maps a persisted job to the builder's semantic Message. The job
// carries recipients structurally, so no reconstruction from a header blob is
// required and BCC can never leak into a header through a missing-metadata path.
func messageFromJob(job *models.OutboundJob) Message {
	return Message{
		From:      job.MailFrom,
		To:        job.To,
		CC:        job.CC,
		BCC:       job.BCC,
		Subject:   job.Subject,
		TextBody:  job.TextBody,
		HTMLBody:  job.HTMLBody,
		Headers:   parseCustomHeaders(job.HeadersJSON),
		MessageID: job.MessageIDHeader,
	}
}

// parseCustomHeaders decodes the stored custom-header map. HeadersJSON now holds
// only caller-supplied custom headers (no _to/_cc recipient metadata).
func parseCustomHeaders(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var h map[string]string
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil
	}
	return h
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
