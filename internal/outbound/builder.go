package outbound

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	"tabmail/internal/models"
)

// BuildMIME constructs a complete MIME message from an OutboundJob.
func BuildMIME(job *models.OutboundJob) ([]byte, error) {
	if len(job.RawMIME) > 0 {
		return job.RawMIME, nil
	}

	var buf bytes.Buffer

	// Standard headers.
	writeHeader(&buf, "From", job.MailFrom)
	writeHeader(&buf, "To", strings.Join(job.RcptTo, ", "))
	writeHeader(&buf, "Subject", job.Subject)
	writeHeader(&buf, "Date", time.Now().UTC().Format(time.RFC1123Z))
	writeHeader(&buf, "Message-ID", job.MessageIDHeader)
	writeHeader(&buf, "MIME-Version", "1.0")

	// Custom headers from JSON.
	if len(job.HeadersJSON) > 0 {
		var headers map[string]string
		if err := json.Unmarshal(job.HeadersJSON, &headers); err == nil {
			for k, v := range headers {
				kl := strings.ToLower(k)
				if kl == "from" || kl == "to" || kl == "subject" || kl == "date" || kl == "message-id" || kl == "mime-version" {
					continue
				}
				writeHeader(&buf, k, v)
			}
		}
	}

	hasText := job.TextBody != ""
	hasHTML := job.HTMLBody != ""

	if hasText && hasHTML {
		// multipart/alternative
		w := multipart.NewWriter(&buf)
		writeHeader(&buf, "Content-Type", fmt.Sprintf("multipart/alternative; boundary=%s", w.Boundary()))
		buf.WriteString("\r\n")

		textPart, _ := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/plain; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		textPart.Write([]byte(job.TextBody))

		htmlPart, _ := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/html; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		htmlPart.Write([]byte(job.HTMLBody))

		w.Close()
	} else if hasHTML {
		writeHeader(&buf, "Content-Type", "text/html; charset=utf-8")
		writeHeader(&buf, "Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		buf.WriteString(job.HTMLBody)
	} else {
		writeHeader(&buf, "Content-Type", "text/plain; charset=utf-8")
		writeHeader(&buf, "Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		buf.WriteString(job.TextBody)
	}

	return buf.Bytes(), nil
}

func writeHeader(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(value)
	buf.WriteString("\r\n")
}
