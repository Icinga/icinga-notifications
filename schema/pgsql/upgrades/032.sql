ALTER TABLE channel
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE channel ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_channel_changed_at ON channel(changed_at);

ALTER TABLE contact
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE contact ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_contact_changed_at ON contact(changed_at);

ALTER TABLE contact_address
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n',
    DROP CONSTRAINT contact_address_contact_id_type_key;
ALTER TABLE contact_address ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_contact_address_changed_at ON contact_address(changed_at);

ALTER TABLE contactgroup
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE contactgroup ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_contactgroup_changed_at ON contactgroup(changed_at);

ALTER TABLE contactgroup_member
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE contactgroup_member ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_contactgroup_member_changed_at ON contactgroup_member(changed_at);

ALTER TABLE schedule
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE schedule ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_schedule_changed_at ON schedule(changed_at);

ALTER TABLE rotation
    ALTER COLUMN priority DROP NOT NULL,
    ALTER COLUMN first_handoff DROP NOT NULL,
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n',
    ADD CHECK (deleted = 'y' OR priority IS NOT NULL AND first_handoff IS NOT NULL);
ALTER TABLE rotation ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_rotation_changed_at ON rotation(changed_at);

ALTER TABLE timeperiod
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE timeperiod ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_timeperiod_changed_at ON timeperiod(changed_at);

ALTER TABLE rotation_member
    RENAME CONSTRAINT rotation_member_check TO ck_rotation_member_either_contact_id_or_contactgroup_id;
ALTER TABLE rotation_member
    ALTER COLUMN position DROP NOT NULL,
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n',
    ADD CONSTRAINT ck_rotation_member_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL);
ALTER TABLE rotation_member ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_rotation_member_changed_at ON rotation_member(changed_at);

ALTER TABLE timeperiod_entry
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE timeperiod_entry ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_timeperiod_entry_changed_at ON timeperiod_entry(changed_at);

ALTER TABLE source
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE source ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_source_changed_at ON source(changed_at);

ALTER TABLE rule
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE rule ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_rule_changed_at ON rule(changed_at);
UPDATE rule SET deleted = 'y' WHERE is_active = 'n';
ALTER TABLE rule DROP COLUMN is_active;

ALTER TABLE rule_escalation
    RENAME CONSTRAINT rule_escalation_check TO ck_rule_escalation_not_both_condition_and_fallback_for;
ALTER TABLE rule_escalation
    ALTER COLUMN position DROP NOT NULL,
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n',
    ADD CONSTRAINT ck_rule_escalation_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL);
ALTER TABLE rule_escalation ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_rule_escalation_changed_at ON rule_escalation(changed_at);

ALTER TABLE rule_escalation_recipient
    ADD COLUMN changed_at bigint NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()) * 1000,
    ADD COLUMN deleted boolenum NOT NULL DEFAULT 'n';
ALTER TABLE rule_escalation_recipient ALTER COLUMN changed_at DROP DEFAULT;
CREATE INDEX idx_rule_escalation_recipient_changed_at ON rule_escalation_recipient(changed_at);
