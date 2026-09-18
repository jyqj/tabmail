package companymail

import (
	"bytes"
	"context"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// Compose prepares a draft payload; it never sends or consumes a draft.
// Forwarding copies authorized inbound attachments into the current author's
// destination workspace. Source read and destination send rights are separate.
func (s *Service) Compose(ctx context.Context, actor authz.Actor, mailbox, message, fromMailbox uuid.UUID, mode string) (*company.DraftPayload, error) {
	if mode != "reply" && mode != "reply_all" && mode != "forward" {
		return nil, app.BadRequest("invalid compose mode")
	}
	from, err := s.sender(ctx, actor, fromMailbox)
	if err != nil {
		return nil, err
	}
	env, err := s.inboundEnvelope(ctx, actor, mailbox, message)
	if err != nil {
		return nil, err
	}
	p := company.DraftPayload{To: []string{}, CC: []string{}, Subject: env.GetHeader("Subject"), TextBody: "\n\n--- " + env.GetHeader("From") + " · " + env.GetHeader("Date") + " ---\n" + env.Text}
	seen := map[string]bool{strings.ToLower(from.Mailbox.FullAddress): true}
	parse := func(raw string) ([]string, error) {
		if strings.TrimSpace(raw) == "" {
			return nil, nil
		}
		as, e := mail.ParseAddressList(raw)
		if e != nil {
			return nil, e
		}
		out := []string{}
		for _, a := range as {
			v := strings.ToLower(a.Address)
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		return out, nil
	}
	if mode == "forward" {
		if !strings.HasPrefix(strings.ToLower(p.Subject), "fwd:") {
			p.Subject = "Fwd: " + p.Subject
		}
		files := env.Attachments
		if len(files) > 10 {
			return nil, app.BadRequest("too many attachments to forward; select attachments manually")
		}
		total := 0
		for _, file := range files {
			total += len(file.Content)
		}
		if total > 20*1024*1024 {
			return nil, app.BadRequest("forward attachments exceed 20 MiB")
		}
		for _, file := range files {
			a, err := s.UploadAttachment(ctx, actor, fromMailbox, file.FileName, bytes.NewReader(file.Content))
			if err != nil {
				return nil, err
			}
			p.AttachmentIDs = append(p.AttachmentIDs, a.ID)
		}
	} else {
		replyTo := env.GetHeader("Reply-To")
		if replyTo == "" {
			replyTo = env.GetHeader("From")
		}
		p.To, err = parse(replyTo)
		if err != nil {
			return nil, app.BadRequest("message has an invalid reply address; compose manually")
		}
		if mode == "reply_all" {
			to, err := parse(env.GetHeader("To"))
			if err != nil {
				return nil, app.BadRequest("invalid original To header")
			}
			p.To = append(p.To, to...)
			p.CC, err = parse(env.GetHeader("Cc"))
			if err != nil {
				return nil, app.BadRequest("invalid original Cc header")
			}
		}
		if !strings.HasPrefix(strings.ToLower(p.Subject), "re:") {
			p.Subject = "Re: " + p.Subject
		}
		id := strings.TrimSpace(env.GetHeader("Message-Id"))
		refs := strings.TrimSpace(env.GetHeader("References"))
		if strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") && !strings.ContainsAny(id, "\r\n") && len(id) <= 254 {
			refs = strings.Join(strings.Fields(refs), " ")
			if len(refs) > 600 {
				refs = ""
			}
			p.Headers = map[string]string{"In-Reply-To": id, "References": strings.TrimSpace(refs + " " + id)}
		}
	}
	return &p, nil
}
