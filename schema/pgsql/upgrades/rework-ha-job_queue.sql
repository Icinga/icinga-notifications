DROP TABLE event_queue;
CREATE TABLE job_queue (
    id bytea NOT NULL, -- SHA256 of JSON representation of the envelope.
    last_update bigint NOT NULL,
    state smallint NOT NULL DEFAULT 0, -- pending (0), processing (1), done (2), or error (64).
    envelope text NOT NULL,

    CONSTRAINT pk_job_queue PRIMARY KEY (id)
);

-- These indices are used to speed up the cleanup queries by last_update and state.
CREATE INDEX idx_job_queue_last_update ON job_queue (last_update);
CREATE INDEX idx_job_queue_last_update_state ON job_queue (last_update, state);

CREATE TABLE job_processing_lock (
    object_id bytea NOT NULL, -- No foreign key, object might not exist at this point.
    job_queue_id bytea NOT NULL, -- references job_queue.id

    CONSTRAINT pk_job_processing_lock PRIMARY KEY (object_id),
    CONSTRAINT fk_job_processing_lock_job_queue FOREIGN KEY (job_queue_id) REFERENCES job_queue(id),
    CONSTRAINT uk_job_processing_lock_job_queue_id UNIQUE (job_queue_id)
);
