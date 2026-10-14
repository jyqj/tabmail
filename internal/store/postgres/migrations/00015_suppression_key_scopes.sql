-- +goose Up
-- Align the already-supported suppression HTTP/credential scopes with storage.
-- Coordinate all role binaries: older binaries reject unknown applied versions.
ALTER TABLE tenant_api_keys DROP CONSTRAINT tenant_api_keys_scopes_check;
ALTER TABLE tenant_api_keys ADD CONSTRAINT tenant_api_keys_scopes_check CHECK (
    jsonb_typeof(scopes) = 'array'
    AND jsonb_array_length(scopes) > 0
    AND scopes <@ '["domains:read","domains:write","routes:read","routes:write","mailboxes:read","mailboxes:write","messages:read","messages:write","send:read","send:write","webhooks:read","webhooks:write","suppression:read","suppression:manage"]'::jsonb
);

-- +goose Down
-- Downgrade deliberately refuses if new-scope keys remain; never erase keys.
ALTER TABLE tenant_api_keys DROP CONSTRAINT tenant_api_keys_scopes_check;
ALTER TABLE tenant_api_keys ADD CONSTRAINT tenant_api_keys_scopes_check CHECK (
    jsonb_typeof(scopes) = 'array'
    AND jsonb_array_length(scopes) > 0
    AND scopes <@ '["domains:read","domains:write","routes:read","routes:write","mailboxes:read","mailboxes:write","messages:read","messages:write","send:read","send:write","webhooks:read","webhooks:write"]'::jsonb
);
