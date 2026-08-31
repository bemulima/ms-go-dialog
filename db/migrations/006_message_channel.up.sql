ALTER TABLE dialog_message
    ADD COLUMN channel VARCHAR(32) NOT NULL DEFAULT 'web';

ALTER TABLE dialog_message
    ADD CONSTRAINT chk_dialog_message_channel
        CHECK (channel IN ('web', 'telegram'));
