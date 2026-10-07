package templates

import (
	"context"
	"errors"
	"testing"
)

func TestR5PreviewManagementLastSourceReadCannotOutliveAuthority(t *testing.T) {
	for _, change := range []string{"management-revoked", "mailbox-address"} {
		t.Run(change, func(t *testing.T) {
			f := newPreviewBoundaryFixture(false)
			f.repo.hook = func(port string, call int) {
				if port == "settings" && call == 2 {
					if change == "management-revoked" {
						f.repo.mailbox.CanManage = false
					} else {
						f.repo.mailbox.Mailbox.FullAddress = "renamed@company.test"
					}
				}
			}
			f.assertRejected(t, context.Background())
		})
	}
}

func TestR5PreviewManagementFinalMailboxErrorAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "cancel-and-error"}[cancelled], func(t *testing.T) {
			f := newPreviewBoundaryFixture(false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fault := errors.New("final mailbox lookup failed")
			f.repo.hook = func(port string, call int) {
				if port == "mailbox" && call == 3 {
					f.repo.faults[port] = fault
					if cancelled {
						cancel()
					}
				}
			}
			err := f.assertRejected(t, ctx)
			if !errors.Is(err, fault) || cancelled && !errors.Is(err, context.Canceled) {
				t.Errorf("final release gate lost failure causes: %v", err)
			}
			if f.repo.counts["mailbox"] != 3 || len(f.repo.calls) != 7 || f.repo.calls[len(f.repo.calls)-1] != "mailbox" {
				t.Errorf("final release gate missing or followed by I/O: %v", f.repo.calls)
			}
		})
	}
}
