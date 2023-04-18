ALTER TABLE incident_contact
    DROP CONSTRAINT pk_incident_contact,
    ALTER COLUMN contact_id DROP NOT NULL,
    ADD COLUMN contactgroup_id bigint REFERENCES contactgroup(id),
    ADD COLUMN schedule_id bigint REFERENCES schedule(id),
    ADD CONSTRAINT key_incident_contact_contact UNIQUE (incident_id, contact_id),
    ADD CONSTRAINT key_incident_contact_contactgroup UNIQUE (incident_id, contactgroup_id),
    ADD CONSTRAINT key_incident_contact_schedule UNIQUE (incident_id, schedule_id),
    ADD CONSTRAINT nonnulls_incident_recipients_check CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1);
