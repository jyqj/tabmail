package app

import (
	"errors"
	"testing"

	"tabmail/internal/authz"
)

func TestErrorConstructors(t *testing.T) {
	data := map[string]any{"revision": 7}
	tests := []struct {
		name     string
		err      error
		wantKind ErrorKind
		wantMsg  string
		wantData map[string]any
	}{
		{name: "BadRequest", err: BadRequest("bad"), wantKind: KindBadRequest, wantMsg: "bad"},
		{name: "Forbidden", err: Forbidden("no"), wantKind: KindForbidden, wantMsg: "no"},
		{name: "NotFound", err: NotFound("gone"), wantKind: KindNotFound, wantMsg: "gone"},
		{name: "Conflict", err: Conflict("clash"), wantKind: KindConflict, wantMsg: "clash"},
		{name: "QuotaExceeded", err: QuotaExceeded("quota reached"), wantKind: KindQuotaExceeded, wantMsg: "quota reached"},
		{name: "ConflictWithData", err: ConflictWithData("stale draft", data), wantKind: KindConflict, wantMsg: "stale draft", wantData: data},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appErr, ok := As(tt.err)
			if !ok {
				t.Fatalf("As failed for %v", tt.err)
			}
			if appErr.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", appErr.Kind, tt.wantKind)
			}
			if appErr.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", appErr.Message, tt.wantMsg)
			}
			if tt.wantData == nil && appErr.Data != nil {
				t.Errorf("Data = %v, want nil", appErr.Data)
			}
			if tt.wantData != nil && appErr.Data["revision"] != tt.wantData["revision"] {
				t.Errorf("Data = %v, want %v", appErr.Data, tt.wantData)
			}
		})
	}
}

func TestErrorMessagePrecedence(t *testing.T) {
	cause := errors.New("connection refused")
	if got := (&Error{Kind: KindInternal, Message: "internal server error", Err: cause}).Error(); got != "internal server error" {
		t.Errorf("Message should win, got %q", got)
	}
	if got := (&Error{Kind: KindInternal, Err: cause}).Error(); got != "connection refused" {
		t.Errorf("Err should be fallback, got %q", got)
	}
	if got := (&Error{Kind: KindConflict}).Error(); got != string(KindConflict) {
		t.Errorf("Kind should be last fallback, got %q", got)
	}
}

func TestInternalWrapsWithoutLeaking(t *testing.T) {
	if Internal(nil) != nil {
		t.Fatal("Internal(nil) must stay nil")
	}
	cause := errors.New("pg: deadlock")
	appErr, ok := As(Internal(cause))
	if !ok || appErr.Kind != KindInternal {
		t.Fatalf("Internal should classify as KindInternal, got %+v", appErr)
	}
	if appErr.Message != "internal server error" {
		t.Errorf("cause message should be replaced, got %q", appErr.Message)
	}
	if !errors.Is(Internal(cause), cause) {
		t.Error("cause must remain reachable via errors.Is for server-side logging")
	}
	existing := BadRequest("already classified")
	if got := Internal(existing); got != existing {
		t.Error("Internal must pass through an already-classified app error")
	}
}

func TestFromAuthz(t *testing.T) {
	if FromAuthz(nil) != nil {
		t.Fatal("FromAuthz(nil) must stay nil")
	}
	denial := authz.ErrForbidden("mailbox owner required")
	appErr, ok := As(FromAuthz(denial))
	if !ok || appErr.Kind != KindForbidden || appErr.Message != "mailbox owner required" {
		t.Errorf("authz denial should map to Forbidden preserving message, got %+v", appErr)
	}
	plain := errors.New("boom")
	appErr, ok = As(FromAuthz(plain))
	if !ok || appErr.Kind != KindInternal {
		t.Errorf("non-authz error should map to Internal, got %+v", appErr)
	}
}
