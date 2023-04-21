UPDATE object_extra_tag SET value = '' WHERE value IS NULL;
ALTER TABLE object_extra_tag ALTER COLUMN value SET NOT NULL;

DELETE FROM event WHERE type IS NULL and severity IS NULL;
UPDATE event SET type = 'state' WHERE type IS NULL AND severity IS NOT NULL;
ALTER TABLE event ALTER COLUMN type SET NOT NULL;

ALTER TABLE incident_history ALTER COLUMN message DROP NOT NULL;
