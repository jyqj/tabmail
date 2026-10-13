package postgres_test

import (
	"sync"
	"testing"
)

// Only these top-level tests have been reviewed for independent fresh-DB
// ownership. Their children stay serial and keep all real deadline/lock proofs.
func r5ParallelFreshDBAllowed(name string) bool {
	switch name {
	case "TestCompanyReadPathDoesNotSerializeOnTenantLock",
		"TestR5APIKeyLastUsedIPRoundTrips",
		"TestR5APIKeyUsageLifecycleCascade",
		"TestR5APIKeyUsageMetadataStoreAndWire",
		"TestR5AccessExplanationFailureReturnsNoProjection",
		"TestR5AccessExplanationOrdersPermissionChanges",
		"TestR5AccessExplanationPreservesCanonicalDecisions",
		"TestR5AccessExplanationWaitsForTargetWrites",
		"TestR5ActivationErrorsPostgresCancellationDuringInsertWait",
		"TestR5ActivationErrorsPostgresInsertFailures",
		"TestR5ActivationErrorsPostgresLateAuditUniqueIsInternal",
		"TestR5ActivationErrorsPostgresRelatedUniqueConflicts",
		"TestR5ActivationErrorsPostgresSuccessfulActivation",
		"TestR5ActivationExpiryAfterRealWaitRollsBack",
		"TestR5AtomicRetryAuditWaitExpiryAndCancellation",
		"TestR5AtomicRetryKeyExpiryDuringJobWait",
		"TestR5AttachmentFinishUsesTimeAfterLockWait",
		"TestR5AttachmentGCProgressConcurrentDraftFence",
		"TestR5AttachmentGCTenantLockTimeoutContinuesRound",
		"TestR5CacheWriteVersusPhysicalDeleteLockOrder",
		"TestR5EnqueueAuditWaitExpiryAndCancellation",
		"TestR5EnqueueRechecksAuthorityAfterParentWait",
		"TestR5GlobalProfileFanoutAuthority",
		"TestR5GlobalProfileFanoutRecipients",
		"TestR5GlobalProfileFanoutRequiredRollback",
		"TestR5IndexCompleteRechecksAfterDocumentWait",
		"TestR5IndexCompleteRechecksAfterJobWait",
		"TestR5IndexFailRechecksAfterJobWait",
		"TestR5IngressDeliveryRejectsTargetExpiryAfterAuditWait",
		"TestR5IngressLeaseExpiryAfterTenantWaitRollsBack",
		"TestR5KeyUsageAuthorityAdmissionIsolation",
		"TestR5LegacyContentKeyExpiryAfterMailboxWait",
		"TestR5OutboundReceiptKeyExpiryAfterQueryWait",
		"TestR5OutboundReceiptListMatchesDetailScope",
		"TestR5OutboundReceiptListOrdersMemberFreeze",
		"TestR5OutboundReceiptOwnerlessKeyAndRetryScope",
		"TestR5OutboundReceiptRejectsStalePrincipal",
		"TestR5OutboundReceiptStablePaginationAndEmptyPage",
		"TestR5OutboundReceiptStorageErrorAndTenantIsolation",
		"TestR5PermissionStagePostgresOwnerlessRetryRejections",
		"TestR5PermissionStagePostgresTruthTable",
		"TestR5ReceivedContentEligibilityMatrix",
		"TestR5ReceivedContentRechecksDeadlineAfterAuthorizationWait",
		"TestR5ReceivedContentRechecksExpiryAtObjectBoundaries",
		"TestR5ReceivedConversationMembersAndSearchAreEligible",
		"TestR5ReceivedEligibilityAfterLaterWaits",
		"TestR5ReceivedPersonalHistoricalExpiryIsExempt",
		"TestR5RecoveryRetryRejectsExpiryAfterAuditWait",
		"TestR5RestoreLifecycleCurrentProofVersusGC",
		"TestR5RestoreLifecyclePreservesRecordedDeadline",
		"TestR5RestoreLifecycleRechecksClockAfterWait",
		"TestR5RestoreLifecycleRejectsElapsedDeadlineWithoutEffects",
		"TestR5RestoreLifecycleSourceBusyAndDeleted",
		"TestR5SentMutationRejectsExpiryAfterItemWait",
		"TestR5SentMutationRollsBackExpiryDuringAuditWait",
		"TestR5SentReadUsesTimeAfterIdentityWait",
		"TestR5SentRecipientSnapshotRejectsDeadlineAfterMailboxWait",
		"TestR5WebhookClaimCompletionChecksLeaseAfterRowWait",
		"TestR5WebhookFanoutCompleteCommandsURLLockOrder":
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
		"TestCompanyReadPathDoesNotSerializeOnTenantLock",
		"TestR5APIKeyLastUsedIPRoundTrips",
		"TestR5APIKeyUsageLifecycleCascade",
		"TestR5APIKeyUsageMetadataStoreAndWire",
		"TestR5AccessExplanationFailureReturnsNoProjection",
		"TestR5AccessExplanationOrdersPermissionChanges",
		"TestR5AccessExplanationPreservesCanonicalDecisions",
		"TestR5AccessExplanationWaitsForTargetWrites",
		"TestR5ActivationErrorsPostgresCancellationDuringInsertWait",
		"TestR5ActivationErrorsPostgresInsertFailures",
		"TestR5ActivationErrorsPostgresLateAuditUniqueIsInternal",
		"TestR5ActivationErrorsPostgresRelatedUniqueConflicts",
		"TestR5ActivationErrorsPostgresSuccessfulActivation",
		"TestR5ActivationExpiryAfterRealWaitRollsBack",
		"TestR5AtomicRetryAuditWaitExpiryAndCancellation",
		"TestR5AtomicRetryKeyExpiryDuringJobWait",
		"TestR5AttachmentFinishUsesTimeAfterLockWait",
		"TestR5AttachmentGCProgressConcurrentDraftFence",
		"TestR5AttachmentGCTenantLockTimeoutContinuesRound",
		"TestR5CacheWriteVersusPhysicalDeleteLockOrder",
		"TestR5EnqueueAuditWaitExpiryAndCancellation",
		"TestR5EnqueueRechecksAuthorityAfterParentWait",
		"TestR5GlobalProfileFanoutAuthority",
		"TestR5GlobalProfileFanoutRecipients",
		"TestR5GlobalProfileFanoutRequiredRollback",
		"TestR5IndexCompleteRechecksAfterDocumentWait",
		"TestR5IndexCompleteRechecksAfterJobWait",
		"TestR5IndexFailRechecksAfterJobWait",
		"TestR5IngressDeliveryRejectsTargetExpiryAfterAuditWait",
		"TestR5IngressLeaseExpiryAfterTenantWaitRollsBack",
		"TestR5KeyUsageAuthorityAdmissionIsolation",
		"TestR5LegacyContentKeyExpiryAfterMailboxWait",
		"TestR5OutboundReceiptKeyExpiryAfterQueryWait",
		"TestR5OutboundReceiptListMatchesDetailScope",
		"TestR5OutboundReceiptListOrdersMemberFreeze",
		"TestR5OutboundReceiptOwnerlessKeyAndRetryScope",
		"TestR5OutboundReceiptRejectsStalePrincipal",
		"TestR5OutboundReceiptStablePaginationAndEmptyPage",
		"TestR5OutboundReceiptStorageErrorAndTenantIsolation",
		"TestR5PermissionStagePostgresOwnerlessRetryRejections",
		"TestR5PermissionStagePostgresTruthTable",
		"TestR5ReceivedContentEligibilityMatrix",
		"TestR5ReceivedContentRechecksDeadlineAfterAuthorizationWait",
		"TestR5ReceivedContentRechecksExpiryAtObjectBoundaries",
		"TestR5ReceivedConversationMembersAndSearchAreEligible",
		"TestR5ReceivedEligibilityAfterLaterWaits",
		"TestR5ReceivedPersonalHistoricalExpiryIsExempt",
		"TestR5RecoveryRetryRejectsExpiryAfterAuditWait",
		"TestR5RestoreLifecycleCurrentProofVersusGC",
		"TestR5RestoreLifecyclePreservesRecordedDeadline",
		"TestR5RestoreLifecycleRechecksClockAfterWait",
		"TestR5RestoreLifecycleRejectsElapsedDeadlineWithoutEffects",
		"TestR5RestoreLifecycleSourceBusyAndDeleted",
		"TestR5SentMutationRejectsExpiryAfterItemWait",
		"TestR5SentMutationRollsBackExpiryDuringAuditWait",
		"TestR5SentReadUsesTimeAfterIdentityWait",
		"TestR5SentRecipientSnapshotRejectsDeadlineAfterMailboxWait",
		"TestR5WebhookClaimCompletionChecksLeaseAfterRowWait",
		"TestR5WebhookFanoutCompleteCommandsURLLockOrder",
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
