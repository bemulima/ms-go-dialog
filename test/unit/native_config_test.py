from __future__ import annotations

import contextlib
import importlib.util
import io
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("dialog_native_config", ROOT / "scripts" / "native_config.py")
assert SPEC is not None and SPEC.loader is not None
native_config = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(native_config)


class NativeConfigTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.env_file = Path(self.temp_dir.name) / ".env"
        self.env_file.write_text(
            "\n".join(
                (
                    "DIALOG_NATIVE_HTTP_PORT=18091",
                    "DIALOG_NATIVE_DATABASE=lw_dialog",
                    "LW_NATIVE_POSTGRES_PORT=5432",
                    "LW_NATIVE_NATS_PORT=4222",
                    "LW_NATIVE_CLAMAV_PORT=3310",
                    "USER_NATIVE_HTTP_PORT=18082",
                    "FILESTORAGE_NATIVE_HTTP_PORT=18085",
                    "LW_DIALOG_DB_USER=lw_dialog",
                    "LW_DIALOG_DB_PASSWORD='db p@ss/word'",
                    "LW_FILESTORAGE_DIALOG_TOKEN=filestorage-test-token",
                    "LW_INTERNAL_TOKEN=user-test-token",
                    "LW_DIALOG_TOKEN=inbound-test-token",
                )
            ),
            encoding="utf-8",
        )

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_maps_infrastructure_endpoints_and_keeps_secrets_private(self) -> None:
        output = io.StringIO()
        with patch.dict(os.environ, {"LW_INFRA_ENV_FILE": str(self.env_file)}, clear=True):
            with contextlib.redirect_stdout(output):
                environment = native_config.native_environment()

        self.assertEqual(environment["HTTP_HOST"], "127.0.0.1")
        self.assertEqual(environment["HTTP_PORT"], "18091")
        self.assertEqual(
            environment["DATABASE_URL"],
            "postgres://lw_dialog:db%20p%40ss%2Fword@127.0.0.1:5432/lw_dialog?sslmode=disable",
        )
        self.assertEqual(environment["NATS_URL"], "nats://127.0.0.1:4222")
        self.assertEqual(environment["CLAMAV_ADDRESS"], "127.0.0.1:3310")
        self.assertEqual(environment["USER_SERVICE_BASE_URL"], "http://127.0.0.1:18082")
        self.assertEqual(environment["FILESTORAGE_SERVICE_BASE_URL"], "http://127.0.0.1:18085")
        self.assertEqual(environment["FILESTORAGE_INTERNAL_TOKEN"], "filestorage-test-token")
        self.assertEqual(environment["USER_SERVICE_INTERNAL_TOKEN"], "user-test-token")
        self.assertEqual(environment["INTERNAL_API_TOKEN"], "inbound-test-token")
        self.assertEqual(environment["SERVICE_MODE"], "all")
        for secret in ("db p@ss/word", "filestorage-test-token", "user-test-token", "inbound-test-token"):
            self.assertNotIn(secret, output.getvalue())

    def test_requires_port_registry_assignment(self) -> None:
        self.env_file.write_text("DIALOG_NATIVE_DATABASE=lw_dialog\n", encoding="utf-8")
        with patch.dict(os.environ, {"LW_INFRA_ENV_FILE": str(self.env_file)}, clear=True):
            with self.assertRaisesRegex(ValueError, "DIALOG_NATIVE_HTTP_PORT is required"):
                native_config.native_environment()

    def test_rejects_invalid_registry_port_without_echoing_configuration(self) -> None:
        self.env_file.write_text(
            self.env_file.read_text(encoding="utf-8") + "\nDIALOG_NATIVE_HTTP_PORT=70000\n",
            encoding="utf-8",
        )
        with patch.dict(os.environ, {"LW_INFRA_ENV_FILE": str(self.env_file)}, clear=True):
            with self.assertRaisesRegex(ValueError, "DIALOG_NATIVE_HTTP_PORT must be a TCP port number"):
                native_config.native_environment()

    def test_native_migration_status_generates_only_read_only_ledger_queries(self) -> None:
        bin_dir = Path(self.temp_dir.name) / "bin"
        bin_dir.mkdir()
        psql_stub = bin_dir / "psql"
        psql_stub.write_text(
            "#!/bin/sh\ncat > \"$NATIVE_STATUS_CAPTURE\"\n",
            encoding="utf-8",
        )
        psql_stub.chmod(0o755)
        capture_path = Path(self.temp_dir.name) / "status.sql"
        environment = {
            "PATH": f"{bin_dir}{os.pathsep}{os.environ['PATH']}",
            "NATIVE_STATUS_CAPTURE": str(capture_path),
            "PGHOST": "127.0.0.1",
            "PGPORT": "5432",
            "PGUSER": "lw_dialog",
            "PGDATABASE": "lw_dialog",
            "PGSSLMODE": "disable",
        }
        result = subprocess.run(
            ["sh", str(ROOT / "scripts" / "native_migrate_status.sh")],
            env=environment,
            check=False,
            capture_output=True,
            text=True,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        generated_sql = capture_path.read_text(encoding="utf-8")
        self.assertIn("BEGIN TRANSACTION READ ONLY;", generated_sql)
        self.assertIn("ROLLBACK;", generated_sql)
        self.assertIn("to_regclass('public.dialog_schema_migration')", generated_sql)
        self.assertIn("010_teacher_turn_ordering_v2", generated_sql)
        self.assertIn("011_canonical_student_turn", generated_sql)
        for statement in ("CREATE ", "INSERT ", "UPDATE ", "DELETE ", "DROP ", "ALTER "):
            self.assertNotIn(statement, generated_sql.upper())


if __name__ == "__main__":
    unittest.main()
