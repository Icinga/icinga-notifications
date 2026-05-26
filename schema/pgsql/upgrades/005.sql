ALTER TABLE incident ADD COLUMN mute_reason text DEFAULT NULL;

-- Migrate currently active muted state from object onto its open incident.
UPDATE incident
  SET mute_reason = object.mute_reason
  FROM object
  WHERE object.id = incident.object_id
    AND incident.recovered_at IS NULL
    AND object.mute_reason IS NOT NULL;

ALTER TABLE object DROP COLUMN mute_reason;
