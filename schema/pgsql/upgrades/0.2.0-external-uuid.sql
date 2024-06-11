CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

ALTER TABLE contact ADD COLUMN external_uuid uuid UNIQUE;
ALTER TABLE contactgroup ADD COLUMN external_uuid uuid UNIQUE;
ALTER TABLE channel ADD COLUMN external_uuid uuid UNIQUE;

UPDATE contact SET external_uuid = uuid_generate_v4() WHERE external_uuid IS NULL;
UPDATE contactgroup SET external_uuid = uuid_generate_v4() WHERE external_uuid IS NULL;
UPDATE channel SET external_uuid = uuid_generate_v4() WHERE external_uuid IS NULL;

ALTER TABLE contact ALTER COLUMN external_uuid SET NOT NULL;
ALTER TABLE contactgroup ALTER COLUMN external_uuid SET NOT NULL;
ALTER TABLE channel ALTER COLUMN external_uuid SET NOT NULL;

DROP EXTENSION "uuid-ossp";
