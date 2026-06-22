ALTER TABLE incident_contact ADD COLUMN changed_at bigint;
UPDATE incident_contact SET changed_at = ih.time
  FROM incident_history ih WHERE (
    ih.incident_id,
    COALESCE(ih.contact_id, 0),
    COALESCE(ih.contactgroup_id, 0),
    COALESCE(ih.schedule_id, 0)
  ) = (
    incident_contact.incident_id,
    COALESCE(incident_contact.contact_id, 0),
    COALESCE(incident_contact.contactgroup_id, 0),
    COALESCE(incident_contact.schedule_id, 0)
  );
UPDATE incident_contact SET changed_at = EXTRACT(EPOCH from NOW()) * 1000 WHERE changed_at IS NULL;
ALTER TABLE incident_contact ALTER COLUMN changed_at SET NOT NULL;
