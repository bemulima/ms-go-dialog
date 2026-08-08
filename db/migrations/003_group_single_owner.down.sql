ALTER TABLE dialog
    DROP CONSTRAINT chk_dialog_personal_shape;

ALTER TABLE dialog
    ADD CONSTRAINT chk_dialog_personal_shape CHECK (
        (type = 1 AND personal_key IS NOT NULL AND octet_length(personal_key) = 32 AND title IS NULL AND member_count = 2)
        OR
        (type = 2 AND personal_key IS NULL AND title IS NOT NULL AND char_length(btrim(title)) BETWEEN 1 AND 200 AND member_count BETWEEN 2 AND 1000)
    );
