CREATE TABLE dialog_attachment (
    id UUID PRIMARY KEY,
    dialog_id UUID NOT NULL REFERENCES dialog (id) ON DELETE RESTRICT,
    message_id UUID,
    uploader_id UUID NOT NULL,
    filestorage_id UUID NOT NULL UNIQUE,
    kind SMALLINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,
    mime_type VARCHAR(255) NOT NULL,
    size_bytes BIGINT NOT NULL,
    width INTEGER,
    height INTEGER,
    checksum_sha256 BYTEA,
    original_filename TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    activated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    activation_attempts INTEGER NOT NULL DEFAULT 0,
    activation_next_attempt_at TIMESTAMPTZ,
    delete_attempts INTEGER NOT NULL DEFAULT 0,
    delete_next_attempt_at TIMESTAMPTZ,
    last_error TEXT,
    storage_deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_dialog_attachment_message_same_dialog
        FOREIGN KEY (dialog_id, message_id)
        REFERENCES dialog_message (dialog_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_dialog_attachment_kind CHECK (kind IN (1, 2)),
    CONSTRAINT chk_dialog_attachment_status CHECK (status IN (1, 2, 3, 4, 5, 6)),
    CONSTRAINT chk_dialog_attachment_size CHECK (size_bytes BETWEEN 1 AND 104857600),
    CONSTRAINT chk_dialog_attachment_dimensions CHECK (
        (kind = 1 AND width BETWEEN 1 AND 32768 AND height BETWEEN 1 AND 32768)
        OR
        (kind = 2 AND width IS NULL AND height IS NULL)
    ),
    CONSTRAINT chk_dialog_attachment_checksum CHECK (checksum_sha256 IS NULL OR octet_length(checksum_sha256) = 32),
    CONSTRAINT chk_dialog_attachment_filename CHECK (char_length(btrim(original_filename)) > 0),
    CONSTRAINT chk_dialog_attachment_expiry CHECK (expires_at > created_at),
    CONSTRAINT chk_dialog_attachment_ready CHECK (status <> 4 OR (message_id IS NOT NULL AND activated_at IS NOT NULL)),
    CONSTRAINT chk_dialog_attachment_deleted CHECK (status <> 6 OR deleted_at IS NOT NULL),
    CONSTRAINT chk_dialog_attachment_attempts CHECK (activation_attempts >= 0 AND delete_attempts >= 0)
);

CREATE INDEX idx_dialog_attachment_message
    ON dialog_attachment (dialog_id, message_id, created_at, id);
CREATE INDEX idx_dialog_attachment_uploader_status
    ON dialog_attachment (uploader_id, status, created_at, id);
CREATE INDEX idx_dialog_attachment_activation_work
    ON dialog_attachment (activation_next_attempt_at, id)
    WHERE status IN (2, 3);
CREATE INDEX idx_dialog_attachment_delete_work
    ON dialog_attachment (delete_next_attempt_at, id)
    WHERE status = 6 AND storage_deleted_at IS NULL;
CREATE INDEX idx_dialog_attachment_pending_expiry
    ON dialog_attachment (expires_at, id)
    WHERE status IN (1, 5);
