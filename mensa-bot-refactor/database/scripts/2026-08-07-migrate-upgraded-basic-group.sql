-- One-off repair: re-point the stale basic-group row at the supergroup it was upgraded to.
--
-- BACKGROUND
--
-- Chat -5536498156 was a legacy basic group. Telegram upgraded it to supergroup
-- -1004435267124 on 2026-08-07, which CHANGES THE CHAT ID. Nothing in the bot consumed the
-- migrate_to_chat_id Telegram handed back, so the dead id stayed in `groups`.
--
-- SyncMemberGroups probes EVERY row in `groups` and refuses to stamp users.groups_synced_at
-- unless all of them answer. One unprobeable row therefore failed the sweep for every member
-- on every registration, leaving groups_synced_at NULL across the board and stalling the
-- cleanup sweep for all of them.
--
-- The code now self-heals (handlers.ChatMigrationHandler records upgrades as they happen, and
-- SyncMemberGroups migrates on a MigrateError), but self-healing only fires when someone
-- registers. This script fixes the existing row immediately and deterministically.
--
-- IDEMPOTENT: safe to run twice. If the old row is already gone it changes nothing.
--
-- USAGE
--   psql "$DATABASE_URL" -f 2026-08-07-migrate-upgraded-basic-group.sql
--
-- Note that groups_synced_at is deliberately NOT stamped here — see the end of this file.

BEGIN;

\set old_id -5536498156
\set new_id -1004435267124

-- Make sure the destination exists. It almost certainly does: the bot was added to the
-- upgraded chat and registered it via group_handler, which is exactly why a bare
-- `UPDATE groups SET group_id = ...` would collide on the primary key rather than relying on
-- the FK's ON UPDATE CASCADE.
INSERT INTO groups (group_id) VALUES (:new_id)
ON CONFLICT (group_id) DO NOTHING;

-- Preserve admin status for members who already have a row under the new id, so the merge
-- below can never silently demote someone.
UPDATE groups_associations AS new
SET is_group_admin = true
FROM groups_associations AS old
WHERE new.group_id = :new_id
  AND old.group_id = :old_id
  AND new.user_id = old.user_id
  AND old.is_group_admin;

-- Move everyone who is not already represented under the new id.
UPDATE groups_associations
SET group_id = :new_id
WHERE group_id = :old_id
  AND user_id NOT IN (SELECT user_id FROM groups_associations WHERE group_id = :new_id);

-- Whatever remains are duplicates of rows that already exist under the new id.
DELETE FROM groups_associations WHERE group_id = :old_id;

DELETE FROM groups WHERE group_id = :old_id;

COMMIT;

-- Verification: both queries must return zero rows.
SELECT 'stale group row still present' AS problem, group_id
FROM groups WHERE group_id = :old_id;

SELECT 'stale association still present' AS problem, user_id, group_id
FROM groups_associations WHERE group_id = :old_id;

-- WHAT THIS SCRIPT DELIBERATELY DOES NOT DO
--
-- It does not backfill users.groups_synced_at. That column is a claim that the bot actually
-- probed Telegram for this member's groups; setting it by hand would assert something no one
-- verified, and the cleanup sweep trusts it to decide who may be deleted. Faking it would
-- hand the sweep permission to kick members whose groups were never established.
--
-- Members registered while the stale row existed still have groups_synced_at = NULL, and
-- SyncMemberGroups only ever runs at registration — so they would stay unstamped forever.
-- Set GROUP_SYNC_BACKFILL=true for one boot to re-probe them honestly. Check who is affected:
--
--   SELECT telegram_id, member_number FROM users WHERE groups_synced_at IS NULL;
