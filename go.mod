module github.com/bjl13/open-cognition

// stdlib-only: no external dependencies while internal/pg is in use.
// TODO: when replacing internal/pg with pgx, bump to go 1.25 and add:
//   require github.com/jackc/pgx/v5 v5.9.1 (or latest)
go 1.22
