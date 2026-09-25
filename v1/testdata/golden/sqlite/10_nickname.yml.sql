ALTER TABLE "users" ADD COLUMN "nickname" TEXT;
ALTER TABLE "users" RENAME COLUMN "nickname" TO "handle";
CREATE INDEX "idx_users_handle" ON "users" ("handle");
UPDATE "users" SET "handle" = ? WHERE "role_id" = ?;
DELETE FROM "users" WHERE "active" = ?;
SELECT 1;
