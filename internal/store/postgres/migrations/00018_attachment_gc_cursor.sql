-- +goose Up
-- Internal scheduler progress, not a tenant retention policy. A UUID boundary
-- deliberately has no tenant FK: deleting that tenant must not reset progress.
CREATE TABLE company_attachment_gc_cursor (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
 last_tenant UUID
);
INSERT INTO company_attachment_gc_cursor(singleton,last_tenant) VALUES(TRUE,NULL);

-- +goose Down
DROP TABLE company_attachment_gc_cursor;
