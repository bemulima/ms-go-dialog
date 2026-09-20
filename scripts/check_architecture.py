#!/usr/bin/env python3
"""Fail closed on the Dialog clean-architecture directory and dependency rules."""

from __future__ import annotations

import re
import sys
from pathlib import Path


MODULE = "github.com/bemulima/ms-go-dialog/"
IMPORT_RE = re.compile(r'"([^"\n]+)"')


def directories(path: Path) -> set[str]:
    if not path.is_dir():
        return set()
    return {entry.name for entry in path.iterdir() if entry.is_dir()}


def imports(path: Path) -> set[str]:
    return set(IMPORT_RE.findall(path.read_text(encoding="utf-8")))


def check(root: Path) -> list[str]:
    errors: list[str] = []
    transport = root / "internal/transport"
    infrastructure = root / "internal/infrastructure"
    http = transport / "http"

    if (root / "internal/adapters").exists():
        errors.append("forbidden legacy internal/adapters path exists")

    expected_transport = {"http", "message", "websocket"}
    actual_transport = directories(transport)
    if actual_transport != expected_transport:
        errors.append(
            "internal/transport children must be "
            + ", ".join(sorted(expected_transport))
            + "; found "
            + ", ".join(sorted(actual_transport))
        )

    expected_http = {"api", "admin", "private", "common"}
    actual_http = directories(http)
    if actual_http != expected_http:
        errors.append(
            "internal/transport/http children must be "
            + ", ".join(sorted(expected_http))
            + "; found "
            + ", ".join(sorted(actual_http))
        )
    for contour in ("api", "admin"):
        if directories(http / contour) != {"v1"}:
            errors.append(f"internal/transport/http/{contour} must contain only v1")
    if "handlers" not in directories(http / "private"):
        errors.append("missing internal/transport/http/private/handlers")

    expected_infrastructure = {"filescan", "filestorage", "health", "http", "messaging", "observability", "persistence"}
    actual_infrastructure = directories(infrastructure)
    if actual_infrastructure != expected_infrastructure:
        errors.append(
            "internal/infrastructure children must be "
            + ", ".join(sorted(expected_infrastructure))
            + "; found "
            + ", ".join(sorted(actual_infrastructure))
        )
    for parent, expected in {"http": {"user"}, "messaging": {"nats"}, "persistence": {"postgres"}}.items():
        if directories(infrastructure / parent) != expected:
            errors.append(f"internal/infrastructure/{parent} must contain only {', '.join(sorted(expected))}")

    forbidden_frameworks = ("github.com/go-chi/", "github.com/gorilla/", "github.com/nats-io/", "github.com/jackc/")
    for layer in (root / "internal/domain", root / "internal/usecase"):
        for source in layer.rglob("*.go"):
            if source.name.endswith("_test.go"):
                continue
            for item in imports(source):
                if item.startswith(MODULE + "internal/transport/") or item.startswith(MODULE + "internal/infrastructure/"):
                    errors.append(f"{source.relative_to(root)} imports a concrete layer: {item}")
                if item.startswith(forbidden_frameworks):
                    errors.append(f"{source.relative_to(root)} imports a framework implementation: {item}")

    for source in transport.rglob("*.go"):
        if source.name.endswith("_test.go"):
            continue
        for item in imports(source):
            if item.startswith(MODULE + "internal/infrastructure/"):
                errors.append(f"{source.relative_to(root)} imports infrastructure instead of receiving a port")
    return errors


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
    errors = check(root)
    if errors:
        for error in errors:
            print(f"architecture: {error}", file=sys.stderr)
        return 1
    print("architecture: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
