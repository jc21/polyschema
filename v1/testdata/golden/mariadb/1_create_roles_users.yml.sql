-- 1_create_roles_users.yml:0 mariadb warning: mariadb commits DDL implicitly, so a failed migration can't be fully rolled back
CREATE TABLE IF NOT EXISTS `roles` (
  `id` INTEGER NOT NULL,
  `name` VARCHAR(50) NOT NULL UNIQUE,
  PRIMARY KEY (`id`)
);
CREATE TABLE `users` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `email` VARCHAR(255) NOT NULL,
  `role_id` INTEGER,
  `active` TINYINT(1) NOT NULL DEFAULT 1,
  `balance` DECIMAL(10,2) DEFAULT 0,
  `settings` JSON,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  CONSTRAINT `uq_users_email` UNIQUE (`email`),
  INDEX `idx_users_role` (`role_id`),
  CONSTRAINT `fk_users_role` FOREIGN KEY (`role_id`) REFERENCES `roles` (`id`) ON DELETE SET NULL,
  CONSTRAINT `chk_users_email` CHECK (email <> '')
);
