CREATE TABLE rule_routing (
    id bigserial,
    rule_id bigint NOT NULL REFERENCES rule(id),
    position integer NOT NULL,
    condition text,
    name text, -- if not set, recipients are used as a fallback for display purposes

    CONSTRAINT pk_rule_routing PRIMARY KEY (id),

    UNIQUE (rule_id, position)
);

CREATE TABLE rule_routing_recipient (
    id bigserial,
    rule_routing_id bigint NOT NULL REFERENCES rule_routing(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    channel_id bigint REFERENCES channel(id),

    CONSTRAINT pk_rule_routing_recipient PRIMARY KEY (id),

    CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1)
);

CREATE TABLE notification_history (
    id bigserial,
    incident_id bigint REFERENCES incident(id),
    rule_routing_id bigint REFERENCES rule_routing(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    channel_id bigint REFERENCES channel(id),
    time bigint NOT NULL,
    notification_state notification_state_type,
    sent_at bigint,
    message text,

    CONSTRAINT pk_notification_history PRIMARY KEY (id)
);

DROP TABLE incident_event;
ALTER TABLE incident_history
    ADD COLUMN notification_history_id bigint REFERENCES notification_history(id),
    DROP COLUMN channel_id,
    DROP COLUMN message,
    DROP COLUMN notification_state,
    DROP COLUMN sent_at;
