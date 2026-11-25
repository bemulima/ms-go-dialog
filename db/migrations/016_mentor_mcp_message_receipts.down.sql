LOCK TABLE dialog_mentor_mcp_message_receipt IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM dialog_mentor_mcp_message_receipt) THEN
        RAISE EXCEPTION 'cannot discard retained Mentor MCP message receipts';
    END IF;
END $$;
DROP TABLE dialog_mentor_mcp_message_receipt;
DROP FUNCTION protect_dialog_mentor_mcp_message_receipts();
