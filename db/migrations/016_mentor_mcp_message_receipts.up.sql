CREATE TABLE dialog_mentor_mcp_message_receipt (
    mentor_id uuid NOT NULL,
    request_id uuid NOT NULL,
    message_id uuid NOT NULL UNIQUE REFERENCES dialog_message(id) ON DELETE RESTRICT,
    command_digest text NOT NULL CHECK (command_digest ~ '^[a-f0-9]{64}$'),
    PRIMARY KEY (mentor_id, request_id)
);

CREATE FUNCTION protect_dialog_mentor_mcp_message_receipts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Mentor MCP message receipts are immutable';
END;
$$;
CREATE TRIGGER dialog_mentor_mcp_message_receipt_immutable
    BEFORE UPDATE OR DELETE ON dialog_mentor_mcp_message_receipt
    FOR EACH ROW EXECUTE FUNCTION protect_dialog_mentor_mcp_message_receipts();
CREATE TRIGGER dialog_mentor_mcp_message_receipt_no_truncate
    BEFORE TRUNCATE ON dialog_mentor_mcp_message_receipt
    FOR EACH STATEMENT EXECUTE FUNCTION protect_dialog_mentor_mcp_message_receipts();
