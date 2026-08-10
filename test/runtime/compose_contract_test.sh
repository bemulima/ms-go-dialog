#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
compose_file="$repo_root/docker-compose.yml"

grep -Fq 'image: clamav/clamav-debian:1.4' "$compose_file"
grep -Fq 'CLAMAV_ADDRESS: clamav:3310' "$compose_file"
grep -Fq 'NATS_URL: nats://nats:4222' "$compose_file"
grep -Fq 'FILESTORAGE_SERVICE_BASE_URL: http://ms-filestorage:8080' "$compose_file"
grep -Fq 'USER_SERVICE_BASE_URL: http://ms-user-service:8080' "$compose_file"
grep -Fq '${DIALOG_PORT:-8095}:8080' "$compose_file"
grep -Fq 'DIALOG_PORT=8095' "$repo_root/.env.dist"

docker compose -f "$compose_file" config --quiet

echo "compose runtime contract passed"
