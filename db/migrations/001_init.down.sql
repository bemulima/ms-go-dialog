DROP TABLE dialog_user_block;
DROP TABLE dialog_ws_ticket;
DROP TABLE dialog_outbox;
ALTER TABLE dialog DROP CONSTRAINT fk_dialog_last_message_same_dialog;
DROP TABLE dialog_message;
DROP TABLE dialog_member;
DROP TABLE dialog;
DROP TABLE dialog_space;
