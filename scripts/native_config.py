#!/usr/bin/env python3
"""Run Dialog commands with native endpoints and approved local credentials."""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path
from urllib.parse import quote


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_INFRA_ENV_FILE = ROOT.parents[1] / "learning-platform-infrastructure" / ".env"
KEY = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


def load_dotenv(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for number, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].lstrip()
        key, separator, value = line.partition("=")
        if separator != "=" or not KEY.fullmatch(key.strip()):
            raise ValueError(f"unsupported dotenv syntax at {path}:{number}")
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
            value = value[1:-1]
        values[key.strip()] = value
    return values


def required(values: dict[str, str], name: str) -> str:
    value = values.get(name, "")
    if not value:
        raise ValueError(f"{name} is required by the approved infrastructure environment")
    return value


def port(value: str, name: str) -> str:
    try:
        number = int(value)
    except ValueError as error:
        raise ValueError(f"{name} must be a TCP port number") from error
    if not 1 <= number <= 65535:
        raise ValueError(f"{name} must be a TCP port number")
    return str(number)


def native_environment() -> dict[str, str]:
    configured_path = os.environ.get("LW_INFRA_ENV_FILE")
    infra_env_file = Path(configured_path).expanduser() if configured_path else DEFAULT_INFRA_ENV_FILE
    values = load_dotenv(infra_env_file)
    values.update(
        {
            name: os.environ[name]
            for name in ("DIALOG_NATIVE_HTTP_PORT", "DIALOG_NATIVE_DATABASE")
            if name in os.environ
        }
    )

    database = required(values, "DIALOG_NATIVE_DATABASE")
    native_port = port(required(values, "DIALOG_NATIVE_HTTP_PORT"), "DIALOG_NATIVE_HTTP_PORT")
    postgres_host = "127.0.0.1"
    postgres_port = port(required(values, "LW_NATIVE_POSTGRES_PORT"), "LW_NATIVE_POSTGRES_PORT")
    nats_port = port(required(values, "LW_NATIVE_NATS_PORT"), "LW_NATIVE_NATS_PORT")
    clamav_port = port(required(values, "LW_NATIVE_CLAMAV_PORT"), "LW_NATIVE_CLAMAV_PORT")
    user_port = port(required(values, "USER_NATIVE_HTTP_PORT"), "USER_NATIVE_HTTP_PORT")
    filestorage_port = port(required(values, "FILESTORAGE_NATIVE_HTTP_PORT"), "FILESTORAGE_NATIVE_HTTP_PORT")

    db_user = required(values, "LW_DIALOG_DB_USER")
    db_password = values.get("LW_DIALOG_DB_PASSWORD") or required(values, "LW_POSTGRES_PASSWORD")
    database_url = "postgres://{}:{}@{}:{}/{}?sslmode=disable".format(
        quote(db_user, safe=""),
        quote(db_password, safe=""),
        postgres_host,
        postgres_port,
        quote(database, safe=""),
    )

    nats_url = f"nats://127.0.0.1:{nats_port}"
    clamav_address = f"127.0.0.1:{clamav_port}"
    user_service_url = f"http://127.0.0.1:{user_port}"
    filestorage_url = f"http://127.0.0.1:{filestorage_port}"

    environment = dict(os.environ)
    environment.update(
        {
            "HTTP_HOST": "127.0.0.1",
            "HTTP_PORT": native_port,
            "DATABASE_URL": database_url,
            "NATS_URL": nats_url,
            "FILESTORAGE_SERVICE_BASE_URL": filestorage_url,
            "FILESTORAGE_INTERNAL_TOKEN": required(values, "LW_FILESTORAGE_DIALOG_TOKEN"),
            "CLAMAV_ADDRESS": clamav_address,
            "USER_SERVICE_BASE_URL": user_service_url,
            "USER_SERVICE_INTERNAL_TOKEN": required(values, "LW_INTERNAL_TOKEN"),
            "INTERNAL_API_TOKEN": required(values, "LW_DIALOG_TOKEN"),
            "SERVICE_MODE": "all",
            # The migration helper uses these only for the explicit native
            # migration task. The application itself consumes DATABASE_URL.
            "PGHOST": postgres_host,
            "PGPORT": postgres_port,
            "PGUSER": db_user,
            "PGPASSWORD": db_password,
            "PGDATABASE": database,
            "PGSSLMODE": "disable",
        }
    )
    print(
        "native Dialog config: database={} postgres={}:{} nats={} clamav={} user={} filestorage={} http=127.0.0.1:{}".format(
            database,
            postgres_host,
            postgres_port,
            nats_url,
            clamav_address,
            user_service_url,
            filestorage_url,
            native_port,
        ),
        flush=True,
    )
    return environment


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command:
        parser.error("a command is required after --")
    try:
        environment = native_environment()
    except (OSError, ValueError) as error:
        print(f"native Dialog configuration failed: {error}", file=sys.stderr)
        return 2
    os.execvpe(command[0], command, environment)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
