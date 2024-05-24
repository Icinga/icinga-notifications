ALTER TYPE event_type ADD VALUE 'mute' BEFORE 'state';
ALTER TYPE event_type ADD VALUE 'unmute';

ALTER TYPE incident_history_event_type ADD VALUE 'muted' AFTER 'opened';
ALTER TYPE incident_history_event_type ADD VALUE 'unmuted' AFTER 'muted';
ALTER TYPE notification_state_type ADD VALUE 'suppressed' BEFORE 'pending';

ALTER TABLE object ADD COLUMN mute_reason text;

ALTER TABLE event
    ADD COLUMN mute boolenum,
    ADD COLUMN mute_reason text;
