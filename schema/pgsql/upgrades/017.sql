CREATE TYPE notification_state_type AS ENUM ( 'pending', 'sent', 'failed' );
ALTER TABLE incident_history
  ADD COLUMN notification_state notification_state_type,
  ADD COLUMN sent_at bigint;
