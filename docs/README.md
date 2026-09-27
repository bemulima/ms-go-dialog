# Documentation index

This repository is the canonical owner of the Dialog service's business rules, contracts, architecture, and operations.

## Start here

- [Domain map](domain-map.md) — ownership and core domain objects.
- [Business rules](business-rules.md) — dialog, membership, message, read-state, and moderation invariants.
- [Agent contract map](agent-contract-map.md) — task-to-contract routing for implementation work.
- [Frontend/backend contract map](frontend-backend-contract-map.md) — UI-facing integration ownership.

## Contracts

- [HTTP contract](http-contract.md)
- [WebSocket contract](websocket-contract.md)
- [Database contract](database-contract.md)
- [Integration contract](integration-contract.md)
- [Error contract](error-contract.md)
- [Attachment lifecycle](attachment-lifecycle.md)
- [Realtime delivery](realtime-delivery.md)

## Operations

- [Operations](operations.md) — runtime modes, readiness, metrics, and operational constraints.
- [Native development](native-development.md) — loopback runtime, shared infrastructure configuration, and migration commands.

Machine-readable counterparts live under `.ai/contracts/`. Change human and machine-readable forms together when the owned behavior changes.
