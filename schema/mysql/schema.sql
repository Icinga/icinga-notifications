CREATE TABLE available_channel_type (
    type varchar(255) NOT NULL,
    name text NOT NULL,
    version text NOT NULL,
    author text NOT NULL,
    config_attrs mediumtext NOT NULL,

    CONSTRAINT pk_available_channel_type PRIMARY KEY (type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE channel (
    id bigint NOT NULL AUTO_INCREMENT,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,
    type varchar(255) NOT NULL, -- 'email', 'sms', ...
    config mediumtext, -- JSON with channel-specific attributes
    -- for now type determines the implementation, in the future, this will need a reference to a concrete
    -- implementation to allow multiple implementations of a sms channel for example, probably even user-provided ones

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_channel PRIMARY KEY (id),
    CONSTRAINT fk_channel_available_channel_type FOREIGN KEY (type) REFERENCES available_channel_type(type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_channel_changed_at ON channel(changed_at);

CREATE TABLE contact (
    id bigint NOT NULL AUTO_INCREMENT,
    full_name text NOT NULL COLLATE utf8mb4_unicode_ci,
    username varchar(254) COLLATE utf8mb4_unicode_ci, -- reference to web user
    default_channel_id bigint NOT NULL,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_contact PRIMARY KEY (id),

    -- As the username is unique, it must be NULLed for deletion via "deleted = 'y'"
    CONSTRAINT uk_contact_username UNIQUE (username),

    CONSTRAINT fk_contact_channel FOREIGN KEY (default_channel_id) REFERENCES channel(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_contact_changed_at ON contact(changed_at);

CREATE TABLE contact_address (
    id bigint NOT NULL AUTO_INCREMENT,
    contact_id bigint NOT NULL,
    type varchar(255) NOT NULL, -- 'phone', 'email', ...
    address text NOT NULL, -- phone number, email address, ...

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_contact_address PRIMARY KEY (id),
    CONSTRAINT fk_contact_address_contact FOREIGN KEY (contact_id) REFERENCES contact(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_contact_address_changed_at ON contact_address(changed_at);

CREATE TABLE contactgroup (
    id bigint NOT NULL AUTO_INCREMENT,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_contactgroup PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_contactgroup_changed_at ON contactgroup(changed_at);

CREATE TABLE contactgroup_member (
    contactgroup_id bigint NOT NULL,
    contact_id bigint NOT NULL,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_contactgroup_member PRIMARY KEY (contactgroup_id, contact_id),
    CONSTRAINT fk_contactgroup_member_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_contactgroup_member_contact FOREIGN KEY (contact_id) REFERENCES contact(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_contactgroup_member_changed_at ON contactgroup_member(changed_at);

CREATE TABLE schedule (
    id bigint NOT NULL AUTO_INCREMENT,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_schedule PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_schedule_changed_at ON schedule(changed_at);

CREATE TABLE rotation (
    id bigint NOT NULL AUTO_INCREMENT,
    schedule_id bigint NOT NULL,
    -- the lower the more important, starting at 0, avoids the need to re-index upon addition
    priority integer,
    name text NOT NULL,
    mode enum('24-7', 'partial', 'multi'), -- NOT NULL is enforced via CHECK not to default to '24-7'
    -- JSON with rotation-specific attributes
    -- Needed exclusively by Web to simplify editing and visualisation
    options mediumtext NOT NULL,

    -- A date in the format 'YYYY-MM-DD' when the first handoff should happen.
    -- It is a string as handoffs are restricted to happen only once per day
    first_handoff date,

    -- Set to the actual time of the first handoff.
    -- If this is in the past during creation of the rotation, it is set to the creation time.
    -- Used by Web to avoid showing shifts that never happened
    actual_handoff bigint NOT NULL,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rotation PRIMARY KEY (id),

    -- Each schedule can only have one rotation with a given priority starting at a given date.
    -- Columns schedule_id, priority, first_handoff must be NULLed for deletion via "deleted = 'y'".
    CONSTRAINT uk_rotation_schedule_id_priority_first_handoff UNIQUE (schedule_id, priority, first_handoff),
    CONSTRAINT ck_rotation_non_deleted_needs_priority_first_handoff CHECK (deleted = 'y' OR priority IS NOT NULL AND first_handoff IS NOT NULL),

    CONSTRAINT ck_rotation_mode_notnull CHECK (mode IS NOT NULL),
    CONSTRAINT fk_rotation_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_rotation_changed_at ON rotation(changed_at);

CREATE TABLE timeperiod (
    id bigint NOT NULL AUTO_INCREMENT,
    owned_by_rotation_id bigint, -- nullable for future standalone timeperiods

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_timeperiod PRIMARY KEY (id),
    CONSTRAINT fk_timeperiod_rotation FOREIGN KEY (owned_by_rotation_id) REFERENCES rotation(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_timeperiod_changed_at ON timeperiod(changed_at);

CREATE TABLE rotation_member (
    id bigint NOT NULL AUTO_INCREMENT,
    rotation_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    position integer,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    -- Each position in a rotation can only be used once.
    -- Column position must be NULLed for deletion via "deleted = 'y'".
    CONSTRAINT uk_rotation_member_rotation_id_position UNIQUE (rotation_id, position),

    -- Two UNIQUE constraints prevent duplicate memberships of the same contact or contactgroup in a single rotation.
    -- Multiple NULLs are not considered to be duplicates, so rows with a contact_id but no contactgroup_id are
    -- basically ignored in the UNIQUE constraint over contactgroup_id and vice versa. The CHECK constraint below
    -- ensures that each row has only non-NULL values in one of these constraints.
    CONSTRAINT uk_rotation_member_rotation_id_contact_id UNIQUE (rotation_id, contact_id),
    CONSTRAINT uk_rotation_member_rotation_id_contactgroup_id UNIQUE (rotation_id, contactgroup_id),

    CONSTRAINT ck_rotation_member_either_contact_id_or_contactgroup_id CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) = 1),
    CONSTRAINT ck_rotation_member_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL),

    CONSTRAINT pk_rotation_member PRIMARY KEY (id),
    CONSTRAINT fk_rotation_member_rotation FOREIGN KEY (rotation_id) REFERENCES rotation(id),
    CONSTRAINT fk_rotation_member_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_rotation_member_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_rotation_member_changed_at ON rotation_member(changed_at);

CREATE TABLE timeperiod_entry (
    id bigint NOT NULL AUTO_INCREMENT,
    timeperiod_id bigint NOT NULL,
    rotation_member_id bigint, -- nullable for future standalone timeperiods
    start_time bigint NOT NULL,
    end_time bigint NOT NULL,
    -- Is needed by icinga-notifications-web to prefilter entries, which matches until this time and should be ignored by the daemon.
    until_time bigint,
    timezone text NOT NULL, -- e.g. 'Europe/Berlin', relevant for evaluating rrule (DST changes differ between zones)
    rrule text, -- recurrence rule (RFC5545)

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_timeperiod_entry PRIMARY KEY (id),
    CONSTRAINT fk_timeperiod_entry_timeperiod FOREIGN KEY (timeperiod_id) REFERENCES timeperiod(id),
    CONSTRAINT fk_timeperiod_entry_rotation_member FOREIGN KEY (rotation_member_id) REFERENCES rotation_member(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_timeperiod_entry_changed_at ON timeperiod_entry(changed_at);

CREATE TABLE source (
    id bigint NOT NULL AUTO_INCREMENT,
    type text NOT NULL,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,
    -- will likely need a distinguishing value for multiple sources of the same type in the future, like for example
    -- the Icinga DB environment ID for Icinga 2 sources

    -- The column listener_password_hash is type-dependent.
    -- This column is required to limit API access for incoming connections to the Listener.
    -- The username will be "source-${id}", allowing early verification.
    listener_password_hash text NOT NULL,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    -- The hash is a PHP password_hash with PASSWORD_DEFAULT algorithm, defaulting to bcrypt. This check roughly ensures
    -- that listener_password_hash can only be populated with bcrypt hashes.
    -- https://icinga.com/docs/icinga-web/latest/doc/20-Advanced-Topics/#manual-user-creation-for-database-authentication-backend
    CONSTRAINT ck_source_bcrypt_listener_password_hash CHECK (listener_password_hash LIKE '$2y$%'),

    CONSTRAINT pk_source PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_source_changed_at ON source(changed_at);

CREATE TABLE object (
    id binary(32) NOT NULL, -- SHA256 of identifying tags and the source.id
    source_id bigint NOT NULL,
    name text NOT NULL,

    url text,
    -- mute_reason indicates whether an object is currently muted by its source, and its non-zero value is mapped to true.
    mute_reason mediumtext,

    CONSTRAINT pk_object PRIMARY KEY (id),
    CONSTRAINT fk_object_source FOREIGN KEY (source_id) REFERENCES source(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE object_id_tag (
    object_id binary(32) NOT NULL,
    tag varchar(255) NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_id_tag PRIMARY KEY (object_id, tag),
    CONSTRAINT fk_object_id_tag_object FOREIGN KEY (object_id) REFERENCES object(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE event (
    id bigint NOT NULL AUTO_INCREMENT,
    time bigint NOT NULL,
    object_id binary(32) NOT NULL,
    -- NOT NULL is enforced via CHECK not to default to 'acknowledgement-cleared'
    type enum('acknowledgement-cleared', 'acknowledgement-set', 'custom', 'downtime-end', 'downtime-removed', 'downtime-start', 'flapping-end', 'flapping-start', 'incident-age', 'mute', 'state', 'unmute'),
    severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg'),
    message mediumtext,
    username text COLLATE utf8mb4_unicode_ci,
    mute enum('n', 'y'),
    mute_reason mediumtext,

    CONSTRAINT pk_event PRIMARY KEY (id),
    CONSTRAINT ck_event_type_notnull CHECK (type IS NOT NULL),
    CONSTRAINT fk_event_object FOREIGN KEY (object_id) REFERENCES object(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE rule (
    id bigint NOT NULL AUTO_INCREMENT,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,
    timeperiod_id bigint,
    source_id bigint NOT NULL, -- the source this rule belongs to
    object_filter text,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule PRIMARY KEY (id),
    CONSTRAINT fk_rule_timeperiod FOREIGN KEY (timeperiod_id) REFERENCES timeperiod(id),
    CONSTRAINT fk_rule_source FOREIGN KEY (source_id) REFERENCES source(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_rule_changed_at ON rule(changed_at);

CREATE TABLE rule_escalation (
    id bigint NOT NULL AUTO_INCREMENT,
    rule_id bigint NOT NULL,
    position integer,
    `condition` text,
    name text COLLATE utf8mb4_unicode_ci, -- if not set, recipients are used as a fallback for display purposes
    fallback_for bigint,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule_escalation PRIMARY KEY (id),

    -- Each position in an escalation can only be used once.
    -- Column position must be NULLed for deletion via "deleted = 'y'"
    CONSTRAINT uk_rule_escalation_rule_id_position UNIQUE (rule_id, position),

    CONSTRAINT ck_rule_escalation_not_both_condition_and_fallback_for CHECK (NOT (`condition` IS NOT NULL AND fallback_for IS NOT NULL)),
    CONSTRAINT ck_rule_escalation_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL),
    CONSTRAINT fk_rule_escalation_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_rule_escalation_rule_escalation FOREIGN KEY (fallback_for) REFERENCES rule_escalation(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_rule_escalation_changed_at ON rule_escalation(changed_at);

CREATE TABLE rule_escalation_recipient (
    id bigint NOT NULL AUTO_INCREMENT,
    rule_escalation_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint,

    changed_at bigint NOT NULL,
    deleted enum('n', 'y') NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule_escalation_recipient PRIMARY KEY (id),
    CONSTRAINT ck_rule_escalation_recipient_has_exactly_one_recipient CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) + if(schedule_id IS NULL, 0, 1) = 1),
    CONSTRAINT fk_rule_escalation_recipient_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id),
    CONSTRAINT fk_rule_escalation_recipient_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_rule_escalation_recipient_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_rule_escalation_recipient_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    CONSTRAINT fk_rule_escalation_recipient_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_rule_escalation_recipient_changed_at ON rule_escalation_recipient(changed_at);

CREATE TABLE incident (
    id bigint NOT NULL AUTO_INCREMENT,
    object_id binary(32) NOT NULL,
    started_at bigint NOT NULL,
    recovered_at bigint,
    -- NOT NULL is enforced via CHECK not to default to 'ok'
    severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg'),

    CONSTRAINT pk_incident PRIMARY KEY (id),
    CONSTRAINT ck_incident_severity_notnull CHECK (severity IS NOT NULL),
    CONSTRAINT fk_incident_object FOREIGN KEY (object_id) REFERENCES object(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_event (
    incident_id bigint NOT NULL,
    event_id bigint NOT NULL,

    CONSTRAINT pk_incident_event PRIMARY KEY (incident_id, event_id),
    CONSTRAINT fk_incident_event_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_event_event FOREIGN KEY (event_id) REFERENCES event(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_contact (
    incident_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    role enum('recipient', 'subscriber', 'manager'), -- NOT NULL is enforced via CHECK not to default to 'recipient'

    CONSTRAINT uk_incident_contact_incident_id_contact_id UNIQUE (incident_id, contact_id),
    CONSTRAINT uk_incident_contact_incident_id_contactgroup_id UNIQUE (incident_id, contactgroup_id),
    CONSTRAINT uk_incident_contact_incident_id_schedule_id UNIQUE (incident_id, schedule_id),

    CONSTRAINT ck_incident_contact_has_exactly_one_recipient CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) + if(schedule_id IS NULL, 0, 1) = 1),
    CONSTRAINT ck_incident_contact_role_notnull CHECK (role IS NOT NULL),
    CONSTRAINT fk_incident_contact_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_contact_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_incident_contact_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_incident_contact_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_rule (
    incident_id bigint NOT NULL,
    rule_id bigint NOT NULL,

    CONSTRAINT pk_incident_rule PRIMARY KEY (incident_id, rule_id),
    CONSTRAINT fk_incident_rule_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_rule_rule FOREIGN KEY (rule_id) REFERENCES rule(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_rule_escalation_state (
    incident_id bigint NOT NULL,
    rule_escalation_id bigint NOT NULL,
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_incident_rule_escalation_state PRIMARY KEY (incident_id, rule_escalation_id),
    CONSTRAINT fk_incident_rule_escalation_state_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_rule_escalation_state_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_history (
    id bigint NOT NULL AUTO_INCREMENT,
    incident_id bigint NOT NULL,
    rule_escalation_id bigint,
    event_id bigint,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    rule_id bigint,
    channel_id bigint,
    time bigint NOT NULL,
    message mediumtext,
    -- Order to be honored for events with identical millisecond timestamps.
    -- NOT NULL is enforced via CHECK not to default to 'opened'
    type enum('opened', 'muted', 'unmuted', 'incident_severity_changed', 'rule_matched', 'escalation_triggered', 'recipient_role_changed', 'closed', 'notified'),
    new_severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg'),
    old_severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg'),
    new_recipient_role enum('recipient', 'subscriber', 'manager'),
    old_recipient_role enum('recipient', 'subscriber', 'manager'),
    notification_state enum('suppressed', 'pending', 'sent', 'failed'),
    sent_at bigint,

    CONSTRAINT pk_incident_history PRIMARY KEY (id),
    CONSTRAINT ck_incident_history_type_notnull CHECK (type IS NOT NULL),
    CONSTRAINT fk_incident_history_incident_rule_escalation_state FOREIGN KEY (incident_id, rule_escalation_id) REFERENCES incident_rule_escalation_state(incident_id, rule_escalation_id),
    CONSTRAINT fk_incident_history_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_history_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id),
    CONSTRAINT fk_incident_history_event FOREIGN KEY (event_id) REFERENCES event(id),
    CONSTRAINT fk_incident_history_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_incident_history_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_incident_history_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    CONSTRAINT fk_incident_history_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_incident_history_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_incident_history_time_type ON incident_history(time, type) COMMENT 'Incident History ordered by time/type';

CREATE TABLE browser_session (
    php_session_id varchar(256) NOT NULL,
    username varchar(254) NOT NULL COLLATE utf8mb4_unicode_ci,
    user_agent text NOT NULL,
    authenticated_at bigint NOT NULL,

    CONSTRAINT pk_browser_session PRIMARY KEY (php_session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_browser_session_authenticated_at ON browser_session (authenticated_at DESC);
CREATE INDEX idx_browser_session_username_agent ON browser_session (username, user_agent(512));
