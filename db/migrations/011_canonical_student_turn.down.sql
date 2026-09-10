DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM dialog_canonical_student_turn) THEN
        RAISE EXCEPTION 'cannot roll back canonical student turn ledger while receipt evidence exists';
    END IF;
END
$$;

DROP TABLE dialog_canonical_student_turn;
