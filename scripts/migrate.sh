#!/usr/bin/env sh
set -eu

COMMAND="${1:-status}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-internal/database/migrations}"
DATABASE="${DATABASE_URL:-${PGDATABASE:-}}"

if [ -z "$DATABASE" ]; then
  echo "DATABASE_URL or PGDATABASE is required" >&2
  exit 2
fi

psql_cmd() {
  psql "$DATABASE" "$@"
}

ensure_table() {
  psql_cmd -v ON_ERROR_STOP=1 -q -c "
    CREATE TABLE IF NOT EXISTS public.schema_migrations (
      version text PRIMARY KEY,
      applied_at timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
  "
}

version_from_file() {
  basename "$1" | sed 's/\.up\.sql$//; s/\.down\.sql$//'
}

up() {
  ensure_table
  for file in "$MIGRATIONS_DIR"/*.up.sql; do
    [ -e "$file" ] || continue
    version="$(version_from_file "$file")"
    applied="$(psql_cmd -At -c "SELECT 1 FROM public.schema_migrations WHERE version = '$version'")"
    if [ "$applied" = "1" ]; then
      echo "skip $version"
      continue
    fi
    echo "apply $version"
    psql_cmd -v ON_ERROR_STOP=1 -f "$file"
    psql_cmd -v ON_ERROR_STOP=1 -q -c "INSERT INTO public.schema_migrations (version) VALUES ('$version')"
  done
}

down() {
  ensure_table
  version="$(psql_cmd -At -c "SELECT version FROM public.schema_migrations ORDER BY version DESC LIMIT 1")"
  if [ -z "$version" ]; then
    echo "no applied migrations"
    return
  fi
  file="$MIGRATIONS_DIR/$version.down.sql"
  if [ ! -f "$file" ]; then
    echo "missing down migration: $file" >&2
    exit 1
  fi
  echo "rollback $version"
  psql_cmd -v ON_ERROR_STOP=1 -f "$file"
}

status() {
  ensure_table
  for file in "$MIGRATIONS_DIR"/*.up.sql; do
    [ -e "$file" ] || continue
    version="$(version_from_file "$file")"
    applied="$(psql_cmd -At -c "SELECT applied_at FROM public.schema_migrations WHERE version = '$version'")"
    if [ -n "$applied" ]; then
      echo "$version applied $applied"
    else
      echo "$version pending"
    fi
  done
}

case "$COMMAND" in
  up) up ;;
  down) down ;;
  status) status ;;
  *)
    echo "usage: $0 {up|down|status}" >&2
    exit 2
    ;;
esac
