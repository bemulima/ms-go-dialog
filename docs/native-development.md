# Native macOS development

Dialog can run as a native process while the approved infrastructure repository
provides PostgreSQL, NATS JetStream, ClamAV, User, and FileStorage. The service
configuration remains deployment-neutral; `scripts/native_config.py` supplies
loopback endpoints and credentials from the approved infrastructure `.env`.

## Prepare shared dependencies

From `learning-platform-infrastructure`, use its documented native-infrastructure
workflow to start/check shared dependencies. The Dialog native launcher does not
start containers or alter database state.

The launcher reads `../../learning-platform-infrastructure/.env` by default.
Set `LW_INFRA_ENV_FILE` only when the approved local environment file is stored
elsewhere. It requires the registry assignments `DIALOG_NATIVE_HTTP_PORT` and
`DIALOG_NATIVE_DATABASE`; the infrastructure-owned values are `18091` and
`lw_dialog`. It derives the `DATABASE_URL` privately from
`LW_DIALOG_DB_USER` and `LW_DIALOG_DB_PASSWORD`, falling back to
`LW_POSTGRES_PASSWORD` only when the role-specific password is absent. It never
prints the URL, password, or service tokens.

| Dependency | Endpoint source | Native endpoint |
| --- | --- | --- |
| Dialog HTTP | `DIALOG_NATIVE_HTTP_PORT` | `127.0.0.1:18091` |
| PostgreSQL | `LW_NATIVE_POSTGRES_PORT` | `127.0.0.1:5432` |
| NATS JetStream | `LW_NATIVE_NATS_PORT` | `nats://127.0.0.1:4222` |
| ClamAV | `LW_NATIVE_CLAMAV_PORT` | `127.0.0.1:3310` |
| User | `USER_NATIVE_HTTP_PORT` | `http://127.0.0.1:18082` |
| FileStorage | `FILESTORAGE_NATIVE_HTTP_PORT` | `http://127.0.0.1:18085` |

FileStorage caller authentication comes from `LW_FILESTORAGE_DIALOG_TOKEN`,
User participant resolution uses `LW_INTERNAL_TOKEN`, and Dialog's inbound
internal API uses `LW_DIALOG_TOKEN`. Those trust boundaries remain separate.

## Inspect, migrate, and run

From this repository:

```sh
task migrate:native-status
task migrate:native
task run:native
```

`migrate:native-status` opens only a read-only PostgreSQL transaction and reads
the Dialog migration ledger. It reports each migration as applied, pending, or
unknown when the ledger is absent; it does not create the ledger or inspect
business rows. `migrate:native` is a separate explicit write operation. It uses
the existing ordered migration runner and the same transaction and ledger
behavior as the Docker migration path, selecting the native `psql` transport.
Review the status output before running it. Native application startup does not
apply migrations automatically.

`run:native` binds the HTTP listener to loopback and starts Dialog in `all`
mode. Keep the service-specific Docker Compose stack and its volume-removal
commands separate from this workflow. Stop shared dependencies through the
infrastructure repository's documented non-destructive command.
