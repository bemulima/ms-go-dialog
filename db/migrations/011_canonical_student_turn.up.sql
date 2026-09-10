-- Private, Dialog-owned receipt ledger for the trusted canonical Student-turn
-- command. It deliberately contains only identity/evidence: never the
-- canonical body, browser payload, assistant-ui JSON, or Student state.
CREATE TABLE dialog_canonical_student_turn (
    action_receipt_id UUID PRIMARY KEY,
    canonical_message_command_id UUID NOT NULL UNIQUE,
    dialog_id UUID NOT NULL REFERENCES dialog (id) ON DELETE RESTRICT,
    student_id UUID NOT NULL,
    personal_teacher_id UUID NOT NULL,
    source_prompt_message_id UUID NOT NULL,
    source_prompt_message_version INTEGER NOT NULL,
    block_id VARCHAR(128) NOT NULL,
    action_id VARCHAR(128) NOT NULL,
    source_ui_digest CHAR(64) NOT NULL,
    canonical_student_message_id UUID NOT NULL UNIQUE,
    correlation_id UUID NOT NULL,
    causation_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_dialog_canonical_student_turn_source_action
        UNIQUE (dialog_id, source_prompt_message_id, block_id, action_id),
    CONSTRAINT fk_dialog_canonical_student_turn_source_prompt_same_dialog
        FOREIGN KEY (dialog_id, source_prompt_message_id)
        REFERENCES dialog_message (dialog_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_dialog_canonical_student_turn_message_same_dialog
        FOREIGN KEY (dialog_id, canonical_student_message_id)
        REFERENCES dialog_message (dialog_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_dialog_canonical_student_turn_prompt_version
        CHECK (source_prompt_message_version >= 1),
    CONSTRAINT chk_dialog_canonical_student_turn_block_id
        CHECK (block_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT chk_dialog_canonical_student_turn_action_id
        CHECK (action_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT chk_dialog_canonical_student_turn_source_ui_digest
        CHECK (source_ui_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_dialog_canonical_student_turn_correlation
        CHECK (correlation_id = action_receipt_id),
    CONSTRAINT chk_dialog_canonical_student_turn_causation
        CHECK (causation_id = action_receipt_id)
);

COMMENT ON TABLE dialog_canonical_student_turn IS
    'Private trusted action receipt ledger; never exposed through REST, WebSocket, or event bodies';
COMMENT ON COLUMN dialog_canonical_student_turn.canonical_student_message_id IS
    'Normal immutable Dialog user message materialized exactly once from this receipt';
