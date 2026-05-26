ALTER TABLE incident ADD COLUMN mute_reason mediumtext DEFAULT NULL AFTER severity;

-- Migrate currently active muted state from object onto its open incident.
UPDATE incident
  INNER JOIN object ON object.id = incident.object_id
  SET incident.mute_reason = object.mute_reason
  WHERE incident.recovered_at IS NULL
    AND object.mute_reason IS NOT NULL;

ALTER TABLE object DROP COLUMN mute_reason;
