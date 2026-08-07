CREATE TABLE dialog_space (
    id UUID PRIMARY KEY,
    key VARCHAR(64) NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,
    allowed_origins TEXT[] NOT NULL DEFAULT '{}',
    allow_personal BOOLEAN NOT NULL DEFAULT TRUE,
    allow_groups BOOLEAN NOT NULL DEFAULT TRUE,
    allow_images BOOLEAN NOT NULL DEFAULT TRUE,
    allow_files BOOLEAN NOT NULL DEFAULT TRUE,
    allow_links BOOLEAN NOT NULL DEFAULT TRUE,
    max_group_members INTEGER NOT NULL DEFAULT 500,
    max_body_length INTEGER NOT NULL DEFAULT 10000,
    max_attachments SMALLINT NOT NULL DEFAULT 10,
    max_image_bytes BIGINT NOT NULL DEFAULT 26214400,
    max_file_bytes BIGINT NOT NULL DEFAULT 104857600,
    allowed_file_mime_types TEXT[] NOT NULL DEFAULT ARRAY[
        'application/pdf',
        'text/plain',
        'text/csv',
        'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
    ],
    edit_window_seconds INTEGER NOT NULL DEFAULT 900,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_dialog_space_key CHECK (key ~ '^[a-z0-9][a-z0-9._-]{1,63}$'),
    CONSTRAINT chk_dialog_space_name CHECK (char_length(btrim(name)) > 0),
    CONSTRAINT chk_dialog_space_status CHECK (status IN (0, 1)),
    CONSTRAINT chk_dialog_space_group_members CHECK (max_group_members BETWEEN 2 AND 1000),
    CONSTRAINT chk_dialog_space_body_length CHECK (max_body_length BETWEEN 1 AND 100000),
    CONSTRAINT chk_dialog_space_attachments CHECK (max_attachments BETWEEN 0 AND 20),
    CONSTRAINT chk_dialog_space_image_bytes CHECK (max_image_bytes BETWEEN 1 AND 26214400),
    CONSTRAINT chk_dialog_space_file_bytes CHECK (max_file_bytes BETWEEN 1 AND 104857600),
    CONSTRAINT chk_dialog_space_edit_window CHECK (edit_window_seconds BETWEEN 0 AND 604800)
);

CREATE TABLE dialog (
    id UUID PRIMARY KEY,
    space_id UUID NOT NULL REFERENCES dialog_space (id) ON DELETE RESTRICT,
    type SMALLINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,
    personal_key BYTEA,
    title TEXT,
    created_by UUID NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    member_count INTEGER NOT NULL,
    message_count BIGINT NOT NULL DEFAULT 0,
    max_message_sequence BIGINT NOT NULL DEFAULT 0,
    max_event_sequence BIGINT NOT NULL DEFAULT 0,
    last_message_id UUID,
    last_message_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_dialog_space_id_id UNIQUE (space_id, id),
    CONSTRAINT chk_dialog_type CHECK (type IN (1, 2)),
    CONSTRAINT chk_dialog_status CHECK (status IN (1, 2, 3)),
    CONSTRAINT chk_dialog_personal_shape CHECK (
        (type = 1 AND personal_key IS NOT NULL AND octet_length(personal_key) = 32 AND title IS NULL AND member_count = 2)
        OR
        (type = 2 AND personal_key IS NULL AND title IS NOT NULL AND char_length(btrim(title)) BETWEEN 1 AND 200 AND member_count BETWEEN 2 AND 1000)
    ),
    CONSTRAINT chk_dialog_version CHECK (version >= 1),
    CONSTRAINT chk_dialog_counters CHECK (
        message_count >= 0 AND max_message_sequence >= 0 AND max_event_sequence >= 0
        AND max_event_sequence >= max_message_sequence
    ),
    CONSTRAINT chk_dialog_last_message CHECK (
        (message_count = 0 AND last_message_id IS NULL AND last_message_at IS NULL AND max_message_sequence = 0)
        OR
        (message_count > 0 AND last_message_id IS NOT NULL AND last_message_at IS NOT NULL AND max_message_sequence > 0)
    )
);

CREATE UNIQUE INDEX uq_dialog_personal_pair
    ON dialog (space_id, personal_key)
    WHERE type = 1;
CREATE INDEX idx_dialog_space_activity
    ON dialog (space_id, status, COALESCE(last_message_at, created_at) DESC, id);

CREATE TABLE dialog_member (
    dialog_id UUID NOT NULL REFERENCES dialog (id) ON DELETE RESTRICT,
    user_id UUID NOT NULL,
    role SMALLINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,
    history_from_message_sequence BIGINT NOT NULL DEFAULT 0,
    last_read_message_sequence BIGINT NOT NULL DEFAULT 0,
    unread_count BIGINT NOT NULL DEFAULT 0,
    last_event_sequence BIGINT NOT NULL DEFAULT 0,
    last_read_at TIMESTAMPTZ,
    muted_until TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    added_by UUID NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (dialog_id, user_id),
    CONSTRAINT chk_dialog_member_role CHECK (role IN (1, 2, 3)),
    CONSTRAINT chk_dialog_member_status CHECK (status IN (1, 2, 3)),
    CONSTRAINT chk_dialog_member_sequences CHECK (
        history_from_message_sequence >= 0
        AND last_read_message_sequence >= 0
        AND last_event_sequence >= 0
        AND unread_count >= 0
    ),
    CONSTRAINT chk_dialog_member_left_at CHECK ((status = 1 AND left_at IS NULL) OR (status <> 1 AND left_at IS NOT NULL))
);

CREATE INDEX idx_dialog_member_user_active
    ON dialog_member (user_id, status, dialog_id);
CREATE INDEX idx_dialog_member_dialog_role
    ON dialog_member (dialog_id, status, role, user_id);

CREATE TABLE dialog_message (
    id UUID PRIMARY KEY,
    dialog_id UUID NOT NULL REFERENCES dialog (id) ON DELETE RESTRICT,
    sender_id UUID NOT NULL,
    reply_to_message_id UUID,
    body TEXT NOT NULL DEFAULT '',
    links JSONB NOT NULL DEFAULT '[]'::JSONB,
    status SMALLINT NOT NULL DEFAULT 1,
    version INTEGER NOT NULL DEFAULT 1,
    message_sequence BIGINT NOT NULL,
    last_event_sequence BIGINT NOT NULL,
    idempotency_key UUID NOT NULL,
    edited_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_dialog_message_dialog_id_id UNIQUE (dialog_id, id),
    CONSTRAINT uq_dialog_message_sender_idempotency UNIQUE (sender_id, idempotency_key),
    CONSTRAINT uq_dialog_message_sequence UNIQUE (dialog_id, message_sequence),
    CONSTRAINT fk_dialog_message_reply_same_dialog
        FOREIGN KEY (dialog_id, reply_to_message_id)
        REFERENCES dialog_message (dialog_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_dialog_message_status CHECK (status IN (1, 2, 3)),
    CONSTRAINT chk_dialog_message_version CHECK (version >= 1),
    CONSTRAINT chk_dialog_message_sequences CHECK (message_sequence >= 1 AND last_event_sequence >= message_sequence),
    CONSTRAINT chk_dialog_message_links CHECK (jsonb_typeof(links) = 'array'),
    CONSTRAINT chk_dialog_message_deleted CHECK (status <> 2 OR deleted_at IS NOT NULL)
);

CREATE INDEX idx_dialog_message_cursor
    ON dialog_message (dialog_id, message_sequence DESC, id);
CREATE INDEX idx_dialog_message_changes
    ON dialog_message (dialog_id, last_event_sequence, id);
CREATE INDEX idx_dialog_message_sender
    ON dialog_message (sender_id, created_at DESC, id);

ALTER TABLE dialog
    ADD CONSTRAINT fk_dialog_last_message_same_dialog
    FOREIGN KEY (id, last_message_id)
    REFERENCES dialog_message (dialog_id, id)
    ON DELETE RESTRICT
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE dialog_outbox (
    id UUID PRIMARY KEY,
    dialog_id UUID NOT NULL REFERENCES dialog (id) ON DELETE RESTRICT,
    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id UUID NOT NULL,
    subject VARCHAR(128) NOT NULL,
    event_sequence BIGINT NOT NULL,
    schema_version SMALLINT NOT NULL DEFAULT 1,
    payload JSONB NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_dialog_outbox_sequence UNIQUE (dialog_id, event_sequence),
    CONSTRAINT chk_dialog_outbox_aggregate_type CHECK (char_length(btrim(aggregate_type)) > 0),
    CONSTRAINT chk_dialog_outbox_subject CHECK (subject IN (
        'dialog.created', 'dialog.updated', 'dialog.closed',
        'dialog.member.added', 'dialog.member.removed', 'dialog.member.role_updated',
        'dialog.message.created', 'dialog.message.updated', 'dialog.message.deleted',
        'dialog.message.hidden', 'dialog.message.restored',
        'dialog.attachment.ready', 'dialog.attachment.failed', 'dialog.read.updated'
    )),
    CONSTRAINT chk_dialog_outbox_sequence CHECK (event_sequence >= 1),
    CONSTRAINT chk_dialog_outbox_schema CHECK (schema_version >= 1),
    CONSTRAINT chk_dialog_outbox_payload CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT chk_dialog_outbox_attempts CHECK (attempts >= 0)
);

CREATE INDEX idx_dialog_outbox_pending
    ON dialog_outbox (next_attempt_at, created_at, id)
    WHERE published_at IS NULL;

CREATE TABLE dialog_ws_ticket (
    ticket_hash BYTEA PRIMARY KEY,
    user_id UUID NOT NULL,
    space_id UUID NOT NULL REFERENCES dialog_space (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_dialog_ws_ticket_hash CHECK (octet_length(ticket_hash) = 32),
    CONSTRAINT chk_dialog_ws_ticket_expiry CHECK (expires_at > created_at)
);

CREATE INDEX idx_dialog_ws_ticket_expiry ON dialog_ws_ticket (expires_at, ticket_hash);

CREATE TABLE dialog_user_block (
    blocker_id UUID NOT NULL,
    blocked_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT chk_dialog_user_block_self CHECK (blocker_id <> blocked_id)
);

CREATE INDEX idx_dialog_user_block_target ON dialog_user_block (blocked_id, blocker_id);
