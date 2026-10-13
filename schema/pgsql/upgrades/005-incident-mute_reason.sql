
ALTER TABLE incident ADD COLUMN mute_reason text DEFAULT NULL;

-- Migrate currently active muted state from object onto its open incident.
UPDATE incident
  SET mute_reason = object.mute_reason
  FROM object
  WHERE object.id = incident.object_id
    AND incident.recovered_at IS NULL
    AND object.mute_reason IS NOT NULL;

ALTER TABLE object DROP COLUMN mute_reason;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-5', EXTRACT(EPOCH from NOW()) * 1000);
