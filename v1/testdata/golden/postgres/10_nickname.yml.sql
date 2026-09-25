ALTER TABLE "users" ADD COLUMN "nickname" VARCHAR(50);
ALTER TABLE "users" RENAME COLUMN "nickname" TO "handle";
CREATE INDEX "idx_users_handle" ON "users" ("handle");
UPDATE "users" SET "handle" = $1 WHERE "role_id" = $2;
DELETE FROM "users" WHERE "active" = $1;
COMMENT ON TABLE users IS 'app users';
