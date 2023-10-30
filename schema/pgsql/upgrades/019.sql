CREATE EXTENSION IF NOT EXISTS citext;

ALTER TABLE contact
    ALTER COLUMN full_name TYPE citext,
    ALTER COLUMN username TYPE citext;

ALTER TABLE contactgroup ALTER COLUMN name TYPE citext;
ALTER TABLE schedule ALTER COLUMN name TYPE citext;
ALTER TABLE channel ALTER COLUMN name TYPE citext;
ALTER TABLE source ALTER COLUMN name TYPE citext;
ALTER TABLE event ALTER COLUMN username TYPE citext;
ALTER TABLE rule ALTER COLUMN name TYPE citext;
ALTER TABLE rule_escalation ALTER COLUMN name TYPE citext;
