CREATE TABLE available_channel_type (
    type varchar(255) NOT NULL,
    name text NOT NULL,
    version text NOT NULL,
    author text NOT NULL,
    config_attrs text NOT NULL,

    CONSTRAINT pk_available_channel_type PRIMARY KEY (type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE channel (
    id bigint NOT NULL AUTO_INCREMENT,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,
    type varchar(255) NOT NULL REFERENCES available_channel_type(type), -- 'email', 'sms', ...
    config text, -- JSON with channel-specific attributes
    -- for now type determines the implementation, in the future, this will need a reference to a concrete
    -- implementation to allow multiple implementations of a sms channel for example, probably even user-provided ones

    CONSTRAINT pk_channel PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE contact (
    id bigint NOT NULL AUTO_INCREMENT,
    full_name text NOT NULL COLLATE utf8mb4_unicode_ci,
    username varchar(254) COLLATE utf8mb4_unicode_ci, -- reference to web user
    default_channel_id bigint NOT NULL REFERENCES channel(id),

    CONSTRAINT pk_contact PRIMARY KEY (id),
    UNIQUE (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE contact_address (
    id bigint NOT NULL AUTO_INCREMENT,
    contact_id bigint NOT NULL REFERENCES contact(id),
    type varchar(255) NOT NULL, -- 'phone', 'email', ...
    address text NOT NULL, -- phone number, email address, ...

    CONSTRAINT pk_contact_address PRIMARY KEY (id),
    UNIQUE (contact_id, type) -- constraint may be relaxed in the future to support multiple addresses per type
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE contactgroup (
    id bigint NOT NULL AUTO_INCREMENT,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,

    CONSTRAINT pk_contactgroup PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE contactgroup_member (
    contactgroup_id bigint NOT NULL REFERENCES contactgroup(id),
    contact_id bigint NOT NULL REFERENCES contact(id),

    CONSTRAINT pk_contactgroup_member PRIMARY KEY (contactgroup_id, contact_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE schedule (
    id bigint NOT NULL AUTO_INCREMENT,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,

    CONSTRAINT pk_schedule PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE rotation (
    id bigint NOT NULL AUTO_INCREMENT,
    schedule_id bigint NOT NULL REFERENCES schedule(id),
    -- the lower the more important, starting at 0, avoids the need to re-index upon addition
    priority integer NOT NULL,
    name text NOT NULL,
    mode enum('24-7', 'partial', 'multi') NOT NULL,
    -- JSON with rotation-specific attributes
    -- Needed exclusively by Web to simplify editing and visualisation
    options text NOT NULL,

    -- A date in the format 'YYYY-MM-DD' when the first handoff should happen.
    -- It is a string as handoffs are restricted to happen only once per day
    first_handoff date NOT NULL,

    -- Set to the actual time of the first handoff.
    -- If this is in the past during creation of the rotation, it is set to the creation time.
    -- Used by Web to avoid showing shifts that never happened
    actual_handoff bigint NOT NULL,

    -- each schedule can only have one rotation with a given priority starting at a given date
    UNIQUE (schedule_id, priority, first_handoff),

    CONSTRAINT pk_rotation PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE timeperiod (
    id bigint NOT NULL AUTO_INCREMENT,
    owned_by_schedule_id bigint REFERENCES rotation(id), -- nullable for future standalone timeperiods

    CONSTRAINT pk_timeperiod PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE rotation_member (
    id bigint NOT NULL AUTO_INCREMENT,
    rotation_id bigint NOT NULL REFERENCES rotation(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    position integer NOT NULL,

    UNIQUE (rotation_id, position), -- each position in a rotation can only be used once

    -- Two UNIQUE constraints prevent duplicate memberships of the same contact or contactgroup in a single rotation.
    -- Multiple NULLs are not considered to be duplicates, so rows with a contact_id but no contactgroup_id are
    -- basically ignored in the UNIQUE constraint over contactgroup_id and vice versa. The CHECK constraint below
    -- ensures that each row has only non-NULL values in one of these constraints.
    UNIQUE (rotation_id, contact_id),
    UNIQUE (rotation_id, contactgroup_id),
    CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) = 1),

    CONSTRAINT pk_rotation_member PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE timeperiod_entry (
    id bigint NOT NULL AUTO_INCREMENT,
    timeperiod_id bigint NOT NULL REFERENCES timeperiod(id),
    rotation_member_id bigint REFERENCES rotation_member(id), -- nullable for future standalone timeperiods
    start_time bigint NOT NULL,
    end_time bigint NOT NULL,
    -- Is needed by icinga-notifications-web to prefilter entries, which matches until this time and should be ignored by the daemon.
    until_time bigint,
    timezone text NOT NULL, -- e.g. 'Europe/Berlin', relevant for evaluating rrule (DST changes differ between zones)
    rrule text, -- recurrence rule (RFC5545)

    CONSTRAINT pk_timeperiod_entry PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE source (
    id bigint NOT NULL AUTO_INCREMENT,
    -- The type "icinga2" is special and requires (at least some of) the icinga2_ prefixed columns.
    type text NOT NULL,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,
    -- will likely need a distinguishing value for multiple sources of the same type in the future, like for example
    -- the Icinga DB environment ID for Icinga 2 sources

    -- The column listener_password_hash is type-dependent.
    -- If type is not "icinga2", listener_password_hash is required to limit API access for incoming connections
    -- to the Listener. The username will be "source-${id}", allowing early verification.
    listener_password_hash text,

    -- Following columns are for the "icinga2" type.
    -- At least icinga2_base_url, icinga2_auth_user, and icinga2_auth_pass are required - see CHECK below.
    icinga2_base_url text,
    icinga2_auth_user text,
    icinga2_auth_pass text,
    -- icinga2_ca_pem specifies a custom CA to be used in the PEM format, if not NULL.
    icinga2_ca_pem text,
    -- icinga2_common_name requires Icinga 2's certificate to hold this Common Name if not NULL. This allows using a
    -- differing Common Name - maybe an Icinga 2 Endpoint object name - from the FQDN within icinga2_base_url.
    icinga2_common_name text,
    icinga2_insecure_tls enum('n', 'y') NOT NULL DEFAULT 'n',

    -- The hash is a PHP password_hash with PASSWORD_DEFAULT algorithm, defaulting to bcrypt. This check roughly ensures
    -- that listener_password_hash can only be populated with bcrypt hashes.
    -- https://icinga.com/docs/icinga-web/latest/doc/20-Advanced-Topics/#manual-user-creation-for-database-authentication-backend
    CHECK (listener_password_hash IS NULL OR listener_password_hash LIKE '$2y$%'),
    CHECK (type != 'icinga2' OR (icinga2_base_url IS NOT NULL AND icinga2_auth_user IS NOT NULL AND icinga2_auth_pass IS NOT NULL)),

    CONSTRAINT pk_source PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE object (
    id binary(32) NOT NULL, -- SHA256 of identifying tags and the source.id
    source_id bigint NOT NULL REFERENCES source(id),
    name text NOT NULL,

    url text,
    -- mute_reason indicates whether an object is currently muted by its source, and its non-zero value is mapped to true.
    mute_reason text,

    CONSTRAINT pk_object PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE object_id_tag (
    object_id binary(32) NOT NULL REFERENCES object(id),
    tag varchar(255) NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_id_tag PRIMARY KEY (object_id, tag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE object_extra_tag (
    object_id binary(32) NOT NULL REFERENCES object(id),
    tag varchar(255) NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_extra_tag PRIMARY KEY (object_id, tag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin ROW_FORMAT=DYNAMIC;

CREATE TABLE event (
    id bigint NOT NULL AUTO_INCREMENT,
    time bigint NOT NULL,
    object_id binary(32) NOT NULL REFERENCES object(id),
    type enum('acknowledgement-cleared', 'acknowledgement-set', 'custom', 'downtime-end', 'downtime-removed', 'downtime-start', 'flapping-end', 'flapping-start', 'incident-age', 'mute', 'state', 'unmute') NOT NULL,
    severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg'),
    message text,
    username text COLLATE utf8mb4_unicode_ci,
    mute enum('n', 'y'),
    mute_reason text,

    CONSTRAINT pk_event PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE rule (
    id bigint NOT NULL AUTO_INCREMENT,
    name text NOT NULL COLLATE utf8mb4_unicode_ci,
    timeperiod_id bigint REFERENCES timeperiod(id),
    object_filter text,
    is_active enum('n', 'y') NOT NULL DEFAULT 'y',

    CONSTRAINT pk_rule PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE rule_escalation (
    id bigint NOT NULL AUTO_INCREMENT,
    rule_id bigint NOT NULL REFERENCES rule(id),
    position integer NOT NULL,
    `condition` text,
    name text COLLATE utf8mb4_unicode_ci, -- if not set, recipients are used as a fallback for display purposes
    fallback_for bigint REFERENCES rule_escalation(id),

    CONSTRAINT pk_rule_escalation PRIMARY KEY (id),

    UNIQUE (rule_id, position),
    CHECK (NOT (`condition` IS NOT NULL AND fallback_for IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE rule_escalation_recipient (
    id bigint NOT NULL AUTO_INCREMENT,
    rule_escalation_id bigint NOT NULL REFERENCES rule_escalation(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    channel_id bigint REFERENCES channel(id),

    CONSTRAINT pk_rule_escalation_recipient PRIMARY KEY (id),

    CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) + if(schedule_id IS NULL, 0, 1) = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident (
    id bigint NOT NULL AUTO_INCREMENT,
    object_id binary(32) NOT NULL REFERENCES object(id),
    started_at bigint NOT NULL,
    recovered_at bigint,
    severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg') NOT NULL,

    CONSTRAINT pk_incident PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_event (
    incident_id bigint NOT NULL REFERENCES incident(id),
    event_id bigint NOT NULL REFERENCES event(id),

    CONSTRAINT pk_incident_event PRIMARY KEY (incident_id, event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_contact (
    incident_id bigint NOT NULL REFERENCES incident(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    role enum('recipient', 'subscriber', 'manager') NOT NULL,

    CONSTRAINT key_incident_contact_contact UNIQUE (incident_id, contact_id),
    CONSTRAINT key_incident_contact_contactgroup UNIQUE (incident_id, contactgroup_id),
    CONSTRAINT key_incident_contact_schedule UNIQUE (incident_id, schedule_id),
    CONSTRAINT nonnulls_incident_recipients_check CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) + if(schedule_id IS NULL, 0, 1) = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_rule (
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_id bigint NOT NULL REFERENCES rule(id),

    CONSTRAINT pk_incident_rule PRIMARY KEY (incident_id, rule_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_rule_escalation_state (
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_escalation_id bigint NOT NULL REFERENCES rule_escalation(id),
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_incident_rule_escalation_state PRIMARY KEY (incident_id, rule_escalation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE incident_history (
    id bigint NOT NULL AUTO_INCREMENT,
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_escalation_id bigint REFERENCES rule_escalation(id),
    event_id bigint REFERENCES event(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    rule_id bigint REFERENCES rule(id),
    channel_id bigint REFERENCES channel(id),
    time bigint NOT NULL,
    message text,
    -- Order to be honored for events with identical millisecond timestamps.
    type enum('opened', 'muted', 'unmuted', 'incident_severity_changed', 'rule_matched', 'escalation_triggered', 'recipient_role_changed', 'closed', 'notified') NOT NULL,
    new_severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg'),
    old_severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg'),
    new_recipient_role enum('recipient', 'subscriber', 'manager'),
    old_recipient_role enum('recipient', 'subscriber', 'manager'),
    notification_state enum('suppressed', 'pending', 'sent', 'failed'),
    sent_at bigint,

    CONSTRAINT pk_incident_history PRIMARY KEY (id),
    FOREIGN KEY (incident_id, rule_escalation_id) REFERENCES incident_rule_escalation_state(incident_id, rule_escalation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_incident_history_time_type ON incident_history(time, type) COMMENT 'Incident History ordered by time/type';

CREATE TABLE browser_session (
    php_session_id varchar(256) NOT NULL,
    username varchar(254) NOT NULL COLLATE utf8mb4_unicode_ci,
    user_agent varchar(4096) NOT NULL,
    authenticated_at bigint NOT NULL,

    CONSTRAINT pk_browser_session PRIMARY KEY (php_session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin ROW_FORMAT=DYNAMIC;

CREATE INDEX idx_browser_session_authenticated_at ON browser_session (authenticated_at DESC);
CREATE INDEX idx_browser_session_username_agent ON browser_session (username, user_agent(512));
