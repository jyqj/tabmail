DELETE FROM domain_routes WHERE route_type::text = 'deep_wildcard';
ALTER TABLE domain_routes ALTER COLUMN route_type TYPE TEXT USING route_type::text;
DROP TYPE route_type;
CREATE TYPE route_type AS ENUM ('exact', 'wildcard', 'sequence');
ALTER TABLE domain_routes ALTER COLUMN route_type TYPE route_type USING route_type::route_type;
