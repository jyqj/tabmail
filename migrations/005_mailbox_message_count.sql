ALTER TABLE mailboxes
    ADD COLUMN IF NOT EXISTS message_count BIGINT NOT NULL DEFAULT 0;

UPDATE mailboxes m
SET message_count = sub.count
FROM (
    SELECT mailbox_id, COUNT(*)::BIGINT AS count
    FROM messages
    GROUP BY mailbox_id
) sub
WHERE m.id = sub.mailbox_id;
