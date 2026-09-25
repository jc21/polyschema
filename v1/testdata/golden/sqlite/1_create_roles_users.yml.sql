CREATE TABLE IF NOT EXISTS "roles" (
  "id" INTEGER NOT NULL,
  "name" TEXT NOT NULL UNIQUE,
  PRIMARY KEY ("id")
);
CREATE TABLE "users" (
  "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  "email" TEXT NOT NULL,
  "role_id" INTEGER,
  "active" INTEGER NOT NULL DEFAULT 1,
  "balance" NUMERIC DEFAULT 0,
  "settings" TEXT,
  "created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "uq_users_email" UNIQUE ("email"),
  CONSTRAINT "fk_users_role" FOREIGN KEY ("role_id") REFERENCES "roles" ("id") ON DELETE SET NULL,
  CONSTRAINT "chk_users_email" CHECK (email <> '')
);
CREATE INDEX "idx_users_role" ON "users" ("role_id");
