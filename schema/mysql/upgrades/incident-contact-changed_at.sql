ALTER TABLE incident_contact ADD COLUMN changed_at bigint DEFAULT NULL;
UPDATE incident_contact
  INNER JOIN incident_history ih ON (
    ih.incident_id = incident_contact.incident_id
    AND COALESCE(ih.contact_id, 0) = COALESCE(incident_contact.contact_id, 0)
    AND COALESCE(ih.contactgroup_id, 0) = COALESCE(incident_contact.contactgroup_id, 0)
    AND COALESCE(ih.schedule_id, 0) = COALESCE(incident_contact.schedule_id, 0)
  )
  SET incident_contact.changed_at = ih.time;
ALTER TABLE incident_contact MODIFY COLUMN changed_at bigint NOT NULL;
