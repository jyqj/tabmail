package handlers

import "tabmail/internal/company"

// companyWireValue normalizes required collection fields only at the HTTP
// boundary. Do not add MarshalJSON to the underlying company types: their
// existing JSON encoding is also used for persisted template hashes and draft
// creation receipts. Neither storage bytes nor shared/cached values may change.
// Optional values and unrelated response types deliberately retain their shape.
func companyWireValue(value any) any {
	switch v := value.(type) {
	case *company.Draft:
		if v == nil {
			return v
		}
		out := wireDraft(*v)
		return &out
	case company.Draft:
		return wireDraft(v)
	case []company.Draft:
		out := make([]company.Draft, len(v))
		for i := range v {
			out[i] = wireDraft(v[i])
		}
		return out
	case *company.Template:
		if v == nil {
			return v
		}
		out := wireTemplate(*v)
		return &out
	case company.Template:
		return wireTemplate(v)
	case []company.Template:
		out := make([]company.Template, len(v))
		for i := range v {
			out[i] = wireTemplate(v[i])
		}
		return out
	case *company.TemplateVersion:
		if v == nil {
			return v
		}
		out := wireTemplateVersion(*v)
		return &out
	case company.TemplateVersion:
		return wireTemplateVersion(v)
	case []company.TemplateVersion:
		out := make([]company.TemplateVersion, len(v))
		for i := range v {
			out[i] = wireTemplateVersion(v[i])
		}
		return out
	case *company.DraftPayload:
		if v == nil {
			return v
		}
		out := wireDraftPayload(*v)
		return &out
	case company.DraftPayload:
		return wireDraftPayload(v)
	default:
		return value
	}
}

func wireDraftPayload(v company.DraftPayload) company.DraftPayload {
	if v.To == nil {
		v.To = []string{}
	}
	return v
}

func wireTemplateDraft(v company.TemplateDraft) company.TemplateDraft {
	if v.Variables == nil {
		v.Variables = []company.Variable{}
	}
	return v
}

func wireDraft(v company.Draft) company.Draft {
	v.Payload = wireDraftPayload(v.Payload)
	if v.TemplateVersion != nil && v.TemplateVersion.Snapshot != nil {
		version := *v.TemplateVersion
		snapshot := wireTemplateDraft(*version.Snapshot)
		version.Snapshot = &snapshot
		v.TemplateVersion = &version
	}
	return v
}

func wireTemplate(v company.Template) company.Template {
	v.Draft = wireTemplateDraft(v.Draft)
	return v
}

func wireTemplateVersion(v company.TemplateVersion) company.TemplateVersion {
	v.Snapshot = wireTemplateDraft(v.Snapshot)
	return v
}
