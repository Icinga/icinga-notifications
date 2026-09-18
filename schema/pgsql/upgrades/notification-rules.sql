CREATE TYPE rule_type AS ENUM ('notification', 'escalation');
ALTER TABLE rule ADD COLUMN type rule_type;
UPDATE rule SET type = 'escalation' WHERE type IS NULL;
ALTER TABLE rule ALTER COLUMN type SET NOT NULL;

CREATE TABLE rule_recipient (
    id bigserial,
    rule_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint,

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule_recipient PRIMARY KEY (id),
    CONSTRAINT ck_rule_recipient_has_exactly_one_recipient CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1),
    CONSTRAINT fk_rule_recipient_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_rule_recipient_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_rule_recipient_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_rule_recipient_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    CONSTRAINT fk_rule_recipient_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
);
