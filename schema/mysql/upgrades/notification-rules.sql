ALTER TABLE rule ADD COLUMN type enum('notification', 'escalation') AFTER source_type; -- used for external references
UPDATE rule SET type = 'escalation' WHERE type IS NULL;
ALTER TABLE rule ADD CONSTRAINT ck_rule_type_notnull CHECK (type IS NOT NULL);

CREATE TABLE rule_recipient (
    id bigint NOT NULL AUTO_INCREMENT,
    rule_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule_recipient PRIMARY KEY (id),
    CONSTRAINT ck_rule_recipient_has_exactly_one_recipient CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) + if(schedule_id IS NULL, 0, 1) = 1),
    CONSTRAINT fk_rule_recipient_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_rule_recipient_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_rule_recipient_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_rule_recipient_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    CONSTRAINT fk_rule_recipient_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
