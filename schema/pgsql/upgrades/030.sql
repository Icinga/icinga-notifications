CREATE TYPE event_type AS ENUM (
    'acknowledgement-cleared',
    'acknowledgement-set',
    'custom',
    'downtime-end',
    'downtime-removed',
    'downtime-start',
    'flapping-end',
    'flapping-start',
    'incident-age',
    'state'
    );

UPDATE event SET type = 'incident-age' WHERE type = 'internal';

ALTER TABLE event ALTER COLUMN type TYPE event_type USING type::event_type;
