#!/usr/bin/env bash
set -euo pipefail

gateway_url="${DIALOG_E2E_GATEWAY_URL:-http://localhost:7070}"
admin_gateway_url="${DIALOG_E2E_ADMIN_GATEWAY_URL:-http://localhost:9090}"
origin="${DIALOG_E2E_ORIGIN:-http://localhost:3000}"
admin_token="${DIALOG_E2E_ADMIN_TOKEN:-}"
password="${DIALOG_E2E_PASSWORD:-Dialog-E2E-2026!}"
run_id="$(date +%s)-$$"
repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/ms-go-dialog-e2e.XXXXXX")"

case "$gateway_url" in
  http://*) websocket_url="ws://${gateway_url#http://}" ;;
  https://*) websocket_url="wss://${gateway_url#https://}" ;;
  *) printf 'runtime e2e failed: unsupported gateway URL %s\n' "$gateway_url" >&2; exit 1 ;;
esac

cleanup() {
  case "$work_dir" in
    "${TMPDIR:-/tmp}"/ms-go-dialog-e2e.*) rm -rf -- "$work_dir" ;;
  esac
}
trap cleanup EXIT

fail() {
  printf 'runtime e2e failed: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is missing: $1"
}

expect_status() {
  local expected="$1" actual="$2" response_file="$3" operation="$4"
  if [[ "$actual" != "$expected" ]]; then
    printf '%s returned HTTP %s, expected %s\n' "$operation" "$actual" "$expected" >&2
    jq . "$response_file" >&2 2>/dev/null || true
    exit 1
  fi
}

signup_user() {
  local label="$1" email body http_status tarantool_body code verify_body token
  email="dialog-e2e-${run_id}-${label}@example.test"
  body="$(jq -nc --arg email "$email" --arg password "$password" '{email:$email,password:$password}')"
  http_status="$(curl -sS --max-time 30 -o "$work_dir/${label}-start.json" -w '%{http_code}' \
    -H 'Content-Type: application/json' --data "$body" \
    "$gateway_url/api/auth/v1/auth/signup/start")"
  expect_status 202 "$http_status" "$work_dir/${label}-start.json" "$label signup/start"

  tarantool_body="$(jq -nc --arg email "$email" '{value:{email:$email,password:""}}')"
  http_status="$(curl -sS --max-time 30 -o "$work_dir/${label}-tarantool.json" -w '%{http_code}' \
    -H 'Content-Type: application/json' --data "$tarantool_body" \
    "$gateway_url/api/tarantool/v1/set-new-user")"
  expect_status 200 "$http_status" "$work_dir/${label}-tarantool.json" "$label verification code"
  code="$(jq -er '.code' "$work_dir/${label}-tarantool.json")"

  verify_body="$(jq -nc --arg email "$email" --arg code "$code" '{email:$email,code:$code}')"
  http_status="$(curl -sS --max-time 30 -o "$work_dir/${label}-auth.json" -w '%{http_code}' \
    -H 'Content-Type: application/json' --data "$verify_body" \
    "$gateway_url/api/auth/v1/auth/signup/verify")"
  expect_status 200 "$http_status" "$work_dir/${label}-auth.json" "$label signup/verify"
  token="$(jq -er '.access_token' "$work_dir/${label}-auth.json")"

  http_status="$(curl -sS --max-time 30 -o "$work_dir/${label}-me.json" -w '%{http_code}' \
    -H "Authorization: Bearer $token" "$gateway_url/api/auth/v1/auth/me")"
  expect_status 200 "$http_status" "$work_dir/${label}-me.json" "$label auth/me"
  jq -e '(.data.user_id // .user_id) | strings | length > 0' "$work_dir/${label}-me.json" >/dev/null || fail "$label user id is absent"
}

poll_signed_url() {
  local token="$1" attachment_id="$2" label="$3" http_status=0
  for _ in $(seq 1 20); do
    http_status="$(curl -sS --max-time 30 -o "$work_dir/${label}-signed.json" -w '%{http_code}' \
      -H "Authorization: Bearer $token" \
      "$gateway_url/api/dialog/v1/message-attachment/signed-url/$attachment_id")"
    [[ "$http_status" == 200 ]] && break
    sleep 1
  done
  expect_status 200 "$http_status" "$work_dir/${label}-signed.json" "$label signed URL"
  jq -e '.url | strings | length > 0' "$work_dir/${label}-signed.json" >/dev/null || fail "$label signed URL is empty"
}

for command_name in curl jq go uuidgen base64; do
  require_command "$command_name"
done
[[ -n "$admin_token" ]] || fail "DIALOG_E2E_ADMIN_TOKEN is required"

http_status="$(curl -sS --max-time 30 -o "$work_dir/admin-me.json" -w '%{http_code}' \
  -H "Authorization: Bearer $admin_token" "$gateway_url/api/auth/v1/auth/me")"
expect_status 200 "$http_status" "$work_dir/admin-me.json" "admin auth/me"
admin_id="$(jq -er '.data.user_id // .user_id' "$work_dir/admin-me.json")"

signup_user user1
signup_user user2
user1_token="$(jq -er '.access_token' "$work_dir/user1-auth.json")"
user2_token="$(jq -er '.access_token' "$work_dir/user2-auth.json")"
user2_id="$(jq -er '.data.user_id // .user_id' "$work_dir/user2-me.json")"

space_key="dialog-e2e-${run_id}"
space_body="$(jq -nc --arg key "$space_key" --arg origin "$origin" '{
  key:$key,
  name:"Dialog runtime E2E",
  allowed_origins:[$origin],
  policy:{
    allow_personal:true,allow_groups:true,allow_images:true,allow_files:true,allow_links:true,
    max_group_members:20,max_body_length:10000,max_attachments:5,
    max_image_bytes:1048576,max_file_bytes:1048576,
    allowed_file_mime_types:["text/plain","application/pdf"],edit_window_seconds:900
  }
}')"
http_status="$(curl -sS --max-time 30 -o "$work_dir/space.json" -w '%{http_code}' \
  -H "Authorization: Bearer $admin_token" -H 'Content-Type: application/json' --data "$space_body" \
  "$admin_gateway_url/api/dialog/v1/space/create")"
expect_status 201 "$http_status" "$work_dir/space.json" "admin space/create"
space_id="$(jq -er '.id' "$work_dir/space.json")"

group_body="$(jq -nc --arg space "$space_key" --arg user2 "$user2_id" --arg admin "$admin_id" \
  '{space_key:$space,title:"Runtime E2E group",participant_ids:[$user2,$admin]}')"
http_status="$(curl -sS --max-time 30 -o "$work_dir/group.json" -w '%{http_code}' \
  -H "Authorization: Bearer $user1_token" -H 'Content-Type: application/json' --data "$group_body" \
  "$gateway_url/api/dialog/v1/dialog/group/create")"
expect_status 201 "$http_status" "$work_dir/group.json" "dialog group/create"
jq -e '.member_count == 3' "$work_dir/group.json" >/dev/null || fail "group does not contain three members"
dialog_id="$(jq -er '.id' "$work_dir/group.json")"

for sequence in $(seq 1 20); do
  idempotency_key="$(uuidgen | tr '[:upper:]' '[:lower:]')"
  message_body="$(jq -nc --arg dialog "$dialog_id" --arg key "$idempotency_key" --arg text "message $sequence" \
    '{dialog_id:$dialog,body:$text,attachment_ids:[],idempotency_key:$key}')"
  http_status="$(curl -sS --max-time 30 -o "$work_dir/message.json" -w '%{http_code}' \
    -H "Authorization: Bearer $user1_token" -H 'Content-Type: application/json' --data "$message_body" \
    "$gateway_url/api/dialog/v1/message/create")"
  expect_status 201 "$http_status" "$work_dir/message.json" "message/create $sequence"
done

http_status="$(curl -sS --max-time 30 -o "$work_dir/window.json" -w '%{http_code}' \
  -H "Authorization: Bearer $user2_token" \
  "$gateway_url/api/dialog/v1/message/window?dialog_id=$dialog_id&before=10&after=20")"
expect_status 200 "$http_status" "$work_dir/window.json" "message/window"
jq -e '.items | length == 20' "$work_dir/window.json" >/dev/null || fail "initial window size is not 20"
jq -e '.read_state.unread_count == 20 and .read_state.first_unread_message_sequence == 1' "$work_dir/window.json" >/dev/null || fail "initial unread state is invalid"

read_body='{"through_message_sequence":15}'
http_status="$(curl -sS --max-time 30 -o "$work_dir/read-15.json" -w '%{http_code}' -X PUT \
  -H "Authorization: Bearer $user2_token" -H 'Content-Type: application/json' --data "$read_body" \
  "$gateway_url/api/dialog/v1/dialog/read/$dialog_id")"
expect_status 200 "$http_status" "$work_dir/read-15.json" "dialog read-through 15"
jq -e '.last_read_message_sequence == 15 and .unread_count == 5' "$work_dir/read-15.json" >/dev/null || fail "read-through 15 state is invalid"

for sequence in 16 17; do
  read_body="$(jq -nc --argjson sequence "$sequence" '{through_message_sequence:$sequence}')"
  http_status="$(curl -sS --max-time 30 -o "$work_dir/read-$sequence.json" -w '%{http_code}' -X PUT \
    -H "Authorization: Bearer $user2_token" -H 'Content-Type: application/json' --data "$read_body" \
    "$gateway_url/api/dialog/v1/dialog/read/$dialog_id")"
  expect_status 200 "$http_status" "$work_dir/read-$sequence.json" "dialog read-through $sequence"
done
jq -e '.unread_count == 4' "$work_dir/read-16.json" >/dev/null || fail "read-through 16 did not decrement unread count"
jq -e '.unread_count == 3' "$work_dir/read-17.json" >/dev/null || fail "read-through 17 did not decrement unread count"

http_status="$(curl -sS --max-time 30 -o "$work_dir/admin-dialog.json" -w '%{http_code}' \
  -H "Authorization: Bearer $admin_token" "$gateway_url/api/dialog/v1/dialog/get/$dialog_id")"
expect_status 200 "$http_status" "$work_dir/admin-dialog.json" "admin member dialog/get"
jq -e '.current_member.unread_count == 20' "$work_dir/admin-dialog.json" >/dev/null || fail "another member read state changed"

http_status="$(curl -sS --max-time 30 -o "$work_dir/read-all.json" -w '%{http_code}' -X PUT \
  -H "Authorization: Bearer $user2_token" "$gateway_url/api/dialog/v1/dialog/read-all/$dialog_id")"
expect_status 200 "$http_status" "$work_dir/read-all.json" "dialog read-all"
jq -e '.last_read_message_sequence == 20 and .unread_count == 0' "$work_dir/read-all.json" >/dev/null || fail "read-all state is invalid"

printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=' | base64 -d > "$work_dir/probe.png"
printf 'dialog runtime attachment probe\n' > "$work_dir/probe.txt"

for label in image file; do
  source_file="$work_dir/probe.png"
  content_type='image/png'
  if [[ "$label" == file ]]; then
    source_file="$work_dir/probe.txt"
    content_type='text/plain'
  fi
  http_status="$(curl -sS --max-time 60 -o "$work_dir/$label-upload.json" -w '%{http_code}' \
    -H "Authorization: Bearer $user1_token" -F "dialog_id=$dialog_id" -F "file=@$source_file;type=$content_type" \
    "$gateway_url/api/dialog/v1/message-attachment/upload")"
  expect_status 201 "$http_status" "$work_dir/$label-upload.json" "$label attachment upload"
  attachment_id="$(jq -er '.id' "$work_dir/$label-upload.json")"
  idempotency_key="$(uuidgen | tr '[:upper:]' '[:lower:]')"
  message_body="$(jq -nc --arg dialog "$dialog_id" --arg key "$idempotency_key" --arg attachment "$attachment_id" --arg label "$label" \
    '{dialog_id:$dialog,body:($label + " attachment"),attachment_ids:[$attachment],idempotency_key:$key}')"
  http_status="$(curl -sS --max-time 30 -o "$work_dir/$label-message.json" -w '%{http_code}' \
    -H "Authorization: Bearer $user1_token" -H 'Content-Type: application/json' --data "$message_body" \
    "$gateway_url/api/dialog/v1/message/create")"
  expect_status 201 "$http_status" "$work_dir/$label-message.json" "$label attachment message"
  poll_signed_url "$user2_token" "$attachment_id" "$label"
done

ticket_body="$(jq -nc --arg space "$space_id" '{space_id:$space}')"
for reconnect_attempt in 1 2; do
  http_status="$(curl -sS --max-time 30 -o "$work_dir/ticket-$reconnect_attempt.json" -w '%{http_code}' \
    -H "Authorization: Bearer $user2_token" -H 'Content-Type: application/json' --data "$ticket_body" \
    "$gateway_url/api/dialog/v1/realtime/ticket")"
  expect_status 201 "$http_status" "$work_dir/ticket-$reconnect_attempt.json" "realtime ticket $reconnect_attempt"
  ticket="$(jq -er '.ticket' "$work_dir/ticket-$reconnect_attempt.json")"
  (cd "$repo_root" && go run ./test/runtime/wsprobe \
    --url "$websocket_url/api/dialog/v1/ws" --origin "$origin" --ticket "$ticket" --dialog-id "$dialog_id")
done

printf 'dialog runtime e2e passed: space=%s dialog=%s members=3 messages=22 unread/read-all=passed attachments=2 websocket_reconnect=passed\n' "$space_id" "$dialog_id"
