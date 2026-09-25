-- 10_nickname.yml:0 mariadb warning: mariadb commits DDL implicitly, so a failed migration can't be fully rolled back
ALTER TABLE `users` ADD COLUMN `nickname` VARCHAR(50);
ALTER TABLE `users` RENAME COLUMN `nickname` TO `handle`;
CREATE INDEX `idx_users_handle` ON `users` (`handle`);
UPDATE `users` SET `handle` = ? WHERE `role_id` = ?;
DELETE FROM `users` WHERE `active` = ?;
SELECT 1;
