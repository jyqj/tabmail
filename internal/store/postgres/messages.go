package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

func (s *PgStore) CreateMessage(ctx context.Context, m *models.Message) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.ReceivedAt = time.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE mailboxes SET message_count = message_count + 1 WHERE id=$1`, m.MailboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messages (id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,
			size,seen,raw_object_key,headers_json,received_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		m.ID, m.TenantID, m.MailboxID, m.ZoneID, m.Sender, m.Recipients, m.Subject,
		m.Size, m.Seen, m.RawObjectKey, m.HeadersJSON, m.ReceivedAt, m.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, maxMessages int) (bool, error) {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.ReceivedAt = time.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE mailboxes
		SET message_count = message_count + 1
		WHERE id=$1 AND ($2 <= 0 OR message_count < $2)`,
		m.MailboxID, maxMessages)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messages (id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,
			size,seen,raw_object_key,headers_json,received_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		m.ID, m.TenantID, m.MailboxID, m.ZoneID, m.Sender, m.Recipients, m.Subject,
		m.Size, m.Seen, m.RawObjectKey, m.HeadersJSON, m.ReceivedAt, m.ExpiresAt); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *PgStore) GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error) {
	m := &models.Message{}
	err := s.pool.QueryRow(ctx, `
		SELECT id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,size,seen,
		       raw_object_key,headers_json,received_at,expires_at
		FROM messages WHERE id=$1`, id).
		Scan(&m.ID, &m.TenantID, &m.MailboxID, &m.ZoneID, &m.Sender, &m.Recipients,
			&m.Subject, &m.Size, &m.Seen, &m.RawObjectKey, &m.HeadersJSON,
			&m.ReceivedAt, &m.ExpiresAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (v *pgTenantView) GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error) {
	m := &models.Message{}
	err := v.store.pool.QueryRow(ctx, `
		SELECT id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,size,seen,
		       raw_object_key,headers_json,received_at,expires_at
		FROM messages WHERE id=$1 AND tenant_id=$2`, id, v.tenantID).
		Scan(&m.ID, &m.TenantID, &m.MailboxID, &m.ZoneID, &m.Sender, &m.Recipients,
			&m.Subject, &m.Size, &m.Seen, &m.RawObjectKey, &m.HeadersJSON,
			&m.ReceivedAt, &m.ExpiresAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (s *PgStore) ListMessages(ctx context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error) {
	pg = pg.Normalize()
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE mailbox_id=$1`, mailboxID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,size,seen,
		       raw_object_key,headers_json,received_at,expires_at
		FROM messages WHERE mailbox_id=$1 ORDER BY received_at DESC LIMIT $2 OFFSET $3`,
		mailboxID, pg.PerPage, pg.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.Message
	for rows.Next() {
		m := &models.Message{}
		if err := rows.Scan(&m.ID, &m.TenantID, &m.MailboxID, &m.ZoneID, &m.Sender,
			&m.Recipients, &m.Subject, &m.Size, &m.Seen, &m.RawObjectKey,
			&m.HeadersJSON, &m.ReceivedAt, &m.ExpiresAt); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *PgStore) MarkSeen(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET seen=TRUE WHERE id=$1`, id)
	return err
}

func (s *PgStore) DeleteMessage(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var mailboxID uuid.UUID
	err = tx.QueryRow(ctx, `DELETE FROM messages WHERE id=$1 RETURNING mailbox_id`, id).Scan(&mailboxID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE mailboxes SET message_count = GREATEST(message_count - 1, 0) WHERE id=$1`, mailboxID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) PurgeMailbox(ctx context.Context, mailboxID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE mailbox_id=$1`, mailboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE mailboxes SET message_count = 0 WHERE id=$1`, mailboxID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) CountMessages(ctx context.Context, mailboxID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT message_count FROM mailboxes WHERE id=$1`, mailboxID).Scan(&n)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return n, err
}

func (s *PgStore) CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE raw_object_key=$1`, objectKey).Scan(&n)
	return n, err
}

func (s *PgStore) CountRawObjectReferences(ctx context.Context, objectKey string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM messages WHERE raw_object_key = $1) +
			(SELECT count(*) FROM ingest_jobs WHERE raw_object_key = $1 AND state IN ('pending','retry','processing'))`, objectKey).Scan(&n)
	return n, err
}

func (s *PgStore) CountAllMessages(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&n)
	return n, err
}

func (s *PgStore) CountTenantMessagesSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE tenant_id=$1 AND received_at >= $2`, tenantID, since).Scan(&n)
	return n, err
}

func (s *PgStore) DeleteExpiredMessages(ctx context.Context, before time.Time, limit int) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		WITH doomed AS (
			SELECT id, mailbox_id
			FROM messages
			WHERE expires_at < $1
			ORDER BY expires_at, id
			LIMIT $2
		),
		deleted AS (
			DELETE FROM messages m
			USING doomed d
			WHERE m.id = d.id
			RETURNING d.mailbox_id
		)
		SELECT mailbox_id, count(*) FROM deleted GROUP BY mailbox_id`, before, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var mailboxID uuid.UUID
		var count int
		if err := rows.Scan(&mailboxID, &count); err != nil {
			return 0, err
		}
		total += count
		if _, err := tx.Exec(ctx, `
			UPDATE mailboxes SET message_count = GREATEST(message_count - $2, 0) WHERE id=$1`,
			mailboxID, count); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *PgStore) ListExpiredObjectKeys(ctx context.Context, before time.Time, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT raw_object_key FROM messages
		WHERE expires_at < $1 AND raw_object_key IS NOT NULL AND raw_object_key != ''
		ORDER BY expires_at, id
		LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *PgStore) DeleteExpiredMessagesReturningKeys(ctx context.Context, before time.Time, limit int) (int, []string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		WITH doomed AS (
			SELECT id, mailbox_id, raw_object_key
			FROM messages
			WHERE expires_at < $1
			ORDER BY expires_at, id
			LIMIT $2
		),
		deleted AS (
			DELETE FROM messages m USING doomed d WHERE m.id = d.id
			RETURNING d.mailbox_id, d.raw_object_key
		)
		SELECT mailbox_id, raw_object_key, count(*) FROM deleted GROUP BY mailbox_id, raw_object_key`, before, limit)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	type mailboxDelta struct {
		id    uuid.UUID
		count int
	}

	total := 0
	var keys []string
	var deltas []mailboxDelta
	for rows.Next() {
		var mailboxID uuid.UUID
		var rawKey *string
		var count int
		if err := rows.Scan(&mailboxID, &rawKey, &count); err != nil {
			return 0, nil, err
		}
		total += count
		if rawKey != nil && *rawKey != "" {
			keys = append(keys, *rawKey)
		}
		deltas = append(deltas, mailboxDelta{id: mailboxID, count: count})
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	rows.Close()

	for _, d := range deltas {
		if _, err := tx.Exec(ctx, `
			UPDATE mailboxes SET message_count = GREATEST(message_count - $2, 0) WHERE id=$1`,
			d.id, d.count); err != nil {
			return 0, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return total, keys, nil
}
