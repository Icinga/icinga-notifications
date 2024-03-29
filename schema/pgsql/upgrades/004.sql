ALTER TABLE contact RENAME CONSTRAINT contact_pkey TO pk_contact;
ALTER TABLE contact_address RENAME CONSTRAINT contact_address_pkey TO pk_contact_address;
ALTER TABLE contactgroup RENAME CONSTRAINT contactgroup_pkey TO pk_contactgroup;
ALTER TABLE contactgroup_member RENAME CONSTRAINT contactgroup_member_pkey TO pk_contactgroup_member;
ALTER TABLE timeperiod RENAME CONSTRAINT timeperiod_pkey TO pk_timeperiod;
ALTER TABLE timeperiod_entry RENAME CONSTRAINT timeperiod_entry_pkey TO pk_timeperiod_entry;
ALTER TABLE schedule RENAME CONSTRAINT schedule_pkey TO pk_schedule;
ALTER TABLE channel RENAME CONSTRAINT channel_pkey TO pk_channel;
ALTER TABLE source RENAME CONSTRAINT source_pkey TO pk_source;
ALTER TABLE object RENAME CONSTRAINT object_pkey TO pk_object;
ALTER TABLE source_object RENAME CONSTRAINT source_object_pkey TO pk_source_object;
ALTER TABLE object_extra_tag RENAME CONSTRAINT object_extra_tag_pkey TO pk_object_extra_tag;
ALTER TABLE event RENAME CONSTRAINT event_pkey TO pk_event;
ALTER TABLE rule RENAME CONSTRAINT rule_pkey TO pk_rule;
ALTER TABLE rule_escalation RENAME CONSTRAINT rule_escalation_pkey TO pk_rule_escalation;
ALTER TABLE rule_escalation_recipient RENAME CONSTRAINT rule_escalation_recipient_pkey TO pk_rule_escalation_recipient;
ALTER TABLE incident RENAME CONSTRAINT incident_pkey TO pk_incident;
ALTER TABLE incident_event RENAME CONSTRAINT incident_event_pkey TO pk_incident_event;
ALTER TABLE incident_contact RENAME CONSTRAINT incident_contact_pkey TO pk_incident_contact;
ALTER TABLE incident_rule RENAME CONSTRAINT incident_rule_pkey TO pk_incident_rule;
ALTER TABLE incident_rule_escalation_state RENAME CONSTRAINT incident_rule_escalation_state_pkey TO pk_incident_rule_escalation_state;
ALTER TABLE incident_history RENAME CONSTRAINT incident_history_pkey TO pk_incident_history;
