-- Existing draw records are the round-scoped eligibility ledger. Clearing a
-- round deletes them, restoring eligibility without keeping another identity store.
CREATE INDEX IF NOT EXISTS draws_by_user ON draws(session,generation,user_id);
DELETE FROM entries
WHERE EXISTS (
 SELECT 1 FROM draws
 WHERE draws.session=entries.session AND draws.generation=entries.generation
 AND draws.user_id=entries.user_id
);
CREATE TRIGGER IF NOT EXISTS exclude_round_winners
BEFORE INSERT ON entries
WHEN EXISTS (
 SELECT 1 FROM draws
 WHERE session=NEW.session AND generation=NEW.generation AND user_id=NEW.user_id
)
BEGIN
 SELECT RAISE(IGNORE);
END;
CREATE TRIGGER IF NOT EXISTS prevent_repeat_round_winner
BEFORE INSERT ON draws
WHEN EXISTS (
 SELECT 1 FROM draws
 WHERE session=NEW.session AND generation=NEW.generation AND user_id=NEW.user_id
)
BEGIN
 SELECT RAISE(ABORT,'viewer already won this raffle round');
END;
UPDATE schema_version SET version=2;
