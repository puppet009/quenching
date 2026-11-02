DO $$ BEGIN RAISE EXCEPTION 'Ownership rollback requires a paused deployment and verified backup restoration; automatic down is disabled'; END $$;
