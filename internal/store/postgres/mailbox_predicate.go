package postgres

import "strconv"

// mailboxAliveSQL is the liveness condition for the mailbox alias m. Use the
// wall clock for eligibility, not the transaction's start timestamp: a mailbox
// can expire while a transaction waits for an unrelated lock.
const mailboxAliveSQL = `(m.expires_at IS NULL OR m.expires_at>clock_timestamp())`

// readableMailboxPredicate is the SQL form of EvaluateMailboxAccess's read
// decision after the caller has resolved the effective user and zone scope.
// Fixed SQL identifiers and numeric placeholder positions prevent user input
// from becoming SQL. Tenant isolation is inside the predicate AND must remain
// a separate top-level conjunct in the containing resource query.
//
// Do not use this for management metadata or retention protection: neither is
// a grant to read message content, and send-only metadata remains intentional.
func readableMailboxPredicate(tenantParam, userParam int) string {
	if tenantParam < 1 || userParam < 1 {
		panic("mailbox predicate requires positive bind positions")
	}
	tenant := "$" + strconv.Itoa(tenantParam)
	user := "$" + strconv.Itoa(userParam)
	return `(m.tenant_id=` + tenant + ` AND ` + mailboxAliveSQL +
		` AND (m.owner_user_id=` + user + ` OR EXISTS(SELECT 1 FROM mailbox_grants g WHERE g.tenant_id=m.tenant_id AND g.mailbox_id=m.id AND g.user_id=` + user + ` AND g.can_read)))`
}
