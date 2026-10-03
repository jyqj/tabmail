package postgres_test

import (
	"sync"
	"testing"
)

// Only these top-level tests have been reviewed for independent fresh-DB
// ownership. Their children stay serial and keep all real deadline/lock proofs.
func r5ParallelFreshDBAllowed(name string) bool {
	switch name {
	case "TestR5ActivationExpiryAfterRealWaitRollsBack",
		"TestR5SentMutationRejectsExpiryAfterItemWait",
		"TestR5SentMutationRollsBackExpiryDuringAuditWait",
		"TestR5SentReadUsesTimeAfterIdentityWait",
		"TestR5IndexCompleteRechecksAfterJobWait",
		"TestR5IndexFailRechecksAfterJobWait",
		"TestR5IndexCompleteRechecksAfterDocumentWait",
		"TestR5IngressLeaseExpiryAfterTenantWaitRollsBack",
		"TestR5AttachmentFinishUsesTimeAfterLockWait",
		"TestR5EnqueueRechecksAuthorityAfterParentWait",
		"TestR5EnqueueAuditWaitExpiryAndCancellation",
		"TestR5GlobalProfileFanoutRecipients",
		"TestR5GlobalProfileFanoutRequiredRollback",
		"TestR5GlobalProfileFanoutAuthority",
		"TestR5AccessExplanationOrdersPermissionChanges",
		"TestR5AccessExplanationFailureReturnsNoProjection",
		"TestR5AccessExplanationPreservesCanonicalDecisions",
		"TestR5AccessExplanationWaitsForTargetWrites",
		"TestR5AttachmentGCTenantLockTimeoutContinuesRound":
		return true
	default:
		return false
	}
}

type r5PGAdmission struct{ slots chan struct{} }

func newR5PGAdmission() *r5PGAdmission {
	return &r5PGAdmission{slots: make(chan struct{}, 2)}
}

func (a *r5PGAdmission) acquire() func() {
	a.slots <- struct{}{}
	var once sync.Once
	return func() { once.Do(func() { <-a.slots }) }
}

var r5FreshDBAdmission = newR5PGAdmission()

func r5ParallelFreshDB(t *testing.T) {
	t.Helper()
	if !r5ParallelFreshDBAllowed(t.Name()) {
		t.Fatal("fresh-DB parallel admission requires an exact reviewed top-level test")
	}
	// Never acquire before Parallel: queued top-level tests must first let
	// serial discovery finish. Do not override test.parallel or GOMAXPROCS.
	t.Parallel()
	release := r5FreshDBAdmission.acquire()
	// Registered before any fixture/context/deadline. Parent cleanup runs
	// after children; LIFO keeps this permit until every fixture is cleaned.
	t.Cleanup(func() {
		t.Log("r5_pg_admission: release_after_fixture_cleanup; limit=2")
		release()
	})
	t.Log("r5_pg_admission: acquired_before_fixture; limit=2")
}

// These test admission mechanics only, never PostgreSQL correctness.
func TestR5PGAdmissionExactWhitelist(t *testing.T) {
	for _, name := range []string{
		"TestR5ActivationExpiryAfterRealWaitRollsBack",
		"TestR5SentMutationRejectsExpiryAfterItemWait",
		"TestR5SentMutationRollsBackExpiryDuringAuditWait",
		"TestR5SentReadUsesTimeAfterIdentityWait",
		"TestR5IndexCompleteRechecksAfterJobWait",
		"TestR5IndexFailRechecksAfterJobWait",
		"TestR5IndexCompleteRechecksAfterDocumentWait",
		"TestR5IngressLeaseExpiryAfterTenantWaitRollsBack",
		"TestR5AttachmentFinishUsesTimeAfterLockWait",
		"TestR5EnqueueRechecksAuthorityAfterParentWait",
		"TestR5EnqueueAuditWaitExpiryAndCancellation",
		"TestR5GlobalProfileFanoutRecipients",
		"TestR5GlobalProfileFanoutRequiredRollback",
		"TestR5GlobalProfileFanoutAuthority",
		"TestR5AccessExplanationOrdersPermissionChanges",
		"TestR5AccessExplanationFailureReturnsNoProjection",
		"TestR5AccessExplanationPreservesCanonicalDecisions",
		"TestR5AccessExplanationWaitsForTargetWrites",
		"TestR5AttachmentGCTenantLockTimeoutContinuesRound",
	} {
		if !r5ParallelFreshDBAllowed(name) {
			t.Errorf("reviewed top-level test rejected: %s", name)
		}
		if r5ParallelFreshDBAllowed(name+"/child") || r5ParallelFreshDBAllowed(name+"#01") {
			t.Errorf("nonexact test name admitted: %s", name)
		}
	}
	for _, name := range []string{"", "TestMigrateAppliesCleanly", "TestR5AttachmentGCTenantActualProcessRestart", "TestR5PGAdmissionExactWhitelist"} {
		if r5ParallelFreshDBAllowed(name) {
			t.Errorf("unreviewed test admitted: %s", name)
		}
	}
}

func TestR5PGAdmissionBoundedAndReusable(t *testing.T) {
	a := newR5PGAdmission()
	releaseFirst := a.acquire()
	releaseSecond := a.acquire()
	if len(a.slots) != 2 || cap(a.slots) != 2 {
		t.Fatal("admission is not exactly two lanes")
	}
	select {
	case a.slots <- struct{}{}:
		t.Fatal("third lane admitted while both permits were held")
	default:
	}
	releaseFirst()
	releaseFirst() // duplicate cleanup must not release another test's permit
	if len(a.slots) != 1 {
		t.Fatal("duplicate release consumed the second owner's permit")
	}
	releaseThird := a.acquire()
	if len(a.slots) != 2 {
		t.Fatal("released lane could not be reused")
	}
	releaseSecond()
	releaseThird()
	if len(a.slots) != 0 {
		t.Fatal("admission permits leaked")
	}
}
