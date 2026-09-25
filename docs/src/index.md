---
# https://vitepress.dev/reference/default-theme-home-page
layout: home

hero:
  name: "PolySchema"
  text: Write a migration once. Run it on any database.
  tagline: Portable YAML or JSON schema migrations for PostgreSQL, MySQL, MariaDB and SQLite. Use the Go library or the command line.
  image:
    src: /images/favicon/favicon.svg
    alt: PolySchema Logo
  actions:
    - theme: brand
      text: Get Started
      link: /guide/
    - theme: alt
      text: Install
      link: /setup/
    - theme: alt
      text: GitHub
      link: https://github.com/jc21/polyschema

features:
  - icon: 🗄️
    title: Four engines, one file
    details: Describe tables, columns, indexes, constraints and seed data in YAML or JSON. PolySchema writes the right SQL for PostgreSQL, MySQL, MariaDB and SQLite.
  - icon: 🔍
    title: Checked before it runs
    details: "Run -check to see what each engine can't do before a migration runs. Unsupported operations are refused before anything touches the database."
  - icon: 🧩
    title: Library or CLI
    details: Embed your migrations in a Go binary with embed.FS and pass in your own *sql.DB, or run the standalone polyschema command from CI or a shell.
  - icon: 🔒
    title: Safe to run at startup
    details: Each file runs in a transaction. An advisory lock on Postgres and MySQL stops app instances that start together from migrating twice.
  - icon: 🧾
    title: History and checksums
    details: Each applied file is recorded with its SHA-256 checksum, so a file that was edited after it ran is caught instead of being silently ignored.
  - icon: 🛠️
    title: Raw SQL when you need it
    details: The sql operation takes a different statement for each engine, for anything the portable format doesn't cover.
---
