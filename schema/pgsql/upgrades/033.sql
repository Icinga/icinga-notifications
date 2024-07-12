ALTER TABLE channel RENAME CONSTRAINT channel_type_fkey TO fk_channel_available_channel_type;
ALTER TABLE contact RENAME CONSTRAINT contact_username_key TO uk_contact_username;
ALTER TABLE contact RENAME CONSTRAINT contact_default_channel_id_fkey TO fk_contact_channel;
ALTER TABLE contact_address RENAME CONSTRAINT contact_address_contact_id_fkey TO fk_contact_address_contact;
ALTER TABLE contactgroup_member RENAME CONSTRAINT contactgroup_member_contact_id_fkey TO fk_contactgroup_member_contact;
ALTER TABLE contactgroup_member RENAME CONSTRAINT contactgroup_member_contactgroup_id_fkey TO fk_contactgroup_member_contactgroup;
ALTER TABLE rotation RENAME CONSTRAINT rotation_schedule_id_priority_first_handoff_key TO uk_rotation_schedule_id_priority_first_handoff;
ALTER TABLE rotation RENAME CONSTRAINT rotation_check TO ck_rotation_non_deleted_needs_priority_first_handoff;
ALTER TABLE rotation RENAME CONSTRAINT rotation_schedule_id_fkey TO fk_rotation_schedule;
ALTER TABLE timeperiod RENAME CONSTRAINT timeperiod_owned_by_rotation_id_fkey TO fk_timeperiod_rotation;
ALTER TABLE rotation_member RENAME CONSTRAINT rotation_member_rotation_id_position_key TO uk_rotation_member_rotation_id_position;
ALTER TABLE rotation_member RENAME CONSTRAINT rotation_member_rotation_id_contact_id_key TO uk_rotation_member_rotation_id_contact_id;
ALTER TABLE rotation_member RENAME CONSTRAINT rotation_member_rotation_id_contactgroup_id_key TO uk_rotation_member_rotation_id_contactgroup_id;
ALTER TABLE rotation_member RENAME CONSTRAINT rotation_member_rotation_id_fkey TO fk_rotation_member_rotation;
ALTER TABLE rotation_member RENAME CONSTRAINT rotation_member_contact_id_fkey TO fk_rotation_member_contact;
ALTER TABLE rotation_member RENAME CONSTRAINT rotation_member_contactgroup_id_fkey TO fk_rotation_member_contactgroup;
ALTER TABLE timeperiod_entry RENAME CONSTRAINT timeperiod_entry_timeperiod_id_fkey TO fk_timeperiod_entry_timeperiod;
ALTER TABLE timeperiod_entry RENAME CONSTRAINT timeperiod_entry_rotation_member_id_fkey TO fk_timeperiod_entry_rotation_member;
ALTER TABLE source RENAME CONSTRAINT source_listener_password_hash_check TO ck_source_bcrypt_listener_password_hash;
ALTER TABLE source RENAME CONSTRAINT source_check TO ck_source_icinga2_has_config;
ALTER TABLE object RENAME CONSTRAINT object_id_check TO ck_object_id_is_sha256;
ALTER TABLE object RENAME CONSTRAINT object_source_id_fkey TO fk_object_source;
ALTER TABLE object_id_tag RENAME CONSTRAINT object_id_tag_object_id_fkey TO fk_object_id_tag_object;
ALTER TABLE object_extra_tag RENAME CONSTRAINT object_extra_tag_object_id_fkey TO fk_object_extra_tag_object;
ALTER TABLE event RENAME CONSTRAINT event_object_id_fkey TO fk_event_object;
ALTER TABLE rule RENAME CONSTRAINT rule_timeperiod_id_fkey TO fk_rule_timeperiod;
ALTER TABLE rule_escalation RENAME CONSTRAINT rule_escalation_rule_id_position_key TO uk_rule_escalation_rule_id_position;
ALTER TABLE rule_escalation RENAME CONSTRAINT rule_escalation_rule_id_fkey TO fk_rule_escalation_rule;
ALTER TABLE rule_escalation RENAME CONSTRAINT rule_escalation_fallback_for_fkey TO fk_rule_escalation_rule_escalation;
ALTER TABLE rule_escalation_recipient RENAME CONSTRAINT rule_escalation_recipient_check TO ck_rule_escalation_recipient_has_exactly_one_recipient;
ALTER TABLE rule_escalation_recipient RENAME CONSTRAINT rule_escalation_recipient_rule_escalation_id_fkey TO fk_rule_escalation_recipient_rule_escalation;
ALTER TABLE rule_escalation_recipient RENAME CONSTRAINT rule_escalation_recipient_contact_id_fkey TO fk_rule_escalation_recipient_contact;
ALTER TABLE rule_escalation_recipient RENAME CONSTRAINT rule_escalation_recipient_contactgroup_id_fkey TO fk_rule_escalation_recipient_contactgroup;
ALTER TABLE rule_escalation_recipient RENAME CONSTRAINT rule_escalation_recipient_schedule_id_fkey TO fk_rule_escalation_recipient_schedule;
ALTER TABLE rule_escalation_recipient RENAME CONSTRAINT rule_escalation_recipient_channel_id_fkey TO fk_rule_escalation_recipient_channel;
ALTER TABLE incident RENAME CONSTRAINT incident_object_id_fkey TO fk_incident_object;
ALTER TABLE incident_event RENAME CONSTRAINT incident_event_incident_id_fkey TO fk_incident_event_incident;
ALTER TABLE incident_event RENAME CONSTRAINT incident_event_event_id_fkey TO fk_incident_event_event;
ALTER TABLE incident_contact RENAME CONSTRAINT key_incident_contact_contact TO uk_incident_contact_incident_id_contact_id;
ALTER TABLE incident_contact RENAME CONSTRAINT key_incident_contact_contactgroup TO uk_incident_contact_incident_id_contactgroup_id;
ALTER TABLE incident_contact RENAME CONSTRAINT key_incident_contact_schedule TO uk_incident_contact_incident_id_schedule_id;
ALTER TABLE incident_contact RENAME CONSTRAINT nonnulls_incident_recipients_check TO ck_incident_contact_has_exactly_one_recipient;
ALTER TABLE incident_contact RENAME CONSTRAINT incident_contact_incident_id_fkey TO fk_incident_contact_incident;
ALTER TABLE incident_contact RENAME CONSTRAINT incident_contact_contact_id_fkey TO fk_incident_contact_contact;
ALTER TABLE incident_contact RENAME CONSTRAINT incident_contact_contactgroup_id_fkey TO fk_incident_contact_contactgroup;
ALTER TABLE incident_contact RENAME CONSTRAINT incident_contact_schedule_id_fkey TO fk_incident_contact_schedule;
ALTER TABLE incident_rule RENAME CONSTRAINT incident_rule_incident_id_fkey TO fk_incident_rule_incident;
ALTER TABLE incident_rule RENAME CONSTRAINT incident_rule_rule_id_fkey TO fk_incident_rule_rule;
ALTER TABLE incident_rule_escalation_state RENAME CONSTRAINT incident_rule_escalation_state_incident_id_fkey TO fk_incident_rule_escalation_state_incident;
ALTER TABLE incident_rule_escalation_state RENAME CONSTRAINT incident_rule_escalation_state_rule_escalation_id_fkey TO fk_incident_rule_escalation_state_rule_escalation;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_incident_id_fkey TO fk_incident_history_incident;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_rule_escalation_id_fkey TO fk_incident_history_rule_escalation;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_event_id_fkey TO fk_incident_history_event;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_contact_id_fkey TO fk_incident_history_contact;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_contactgroup_id_fkey TO fk_incident_history_contactgroup;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_schedule_id_fkey TO fk_incident_history_schedule;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_rule_id_fkey TO fk_incident_history_rule;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_channel_id_fkey TO fk_incident_history_channel;

ALTER TABLE incident_history DROP CONSTRAINT IF EXISTS incident_history_incident_id_fkey1; -- PostgreSQL 11
ALTER TABLE incident_history DROP CONSTRAINT IF EXISTS incident_history_incident_id_rule_escalation_id_fkey; -- PostgreSQL 12
ALTER TABLE incident_history ADD CONSTRAINT fk_incident_history_incident_rule_escalation_state FOREIGN KEY (incident_id, rule_escalation_id) REFERENCES incident_rule_escalation_state(incident_id, rule_escalation_id);
