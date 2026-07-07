# MCP Integration Decision

Status: Accepted
Date: 2026-07-07
Scope: confluence2md-indexer integration surface for downstream MCP usage

## Decision

The downstream MCP layer will integrate with confluence2md-indexer by reusing the in-process Go query/index packages as the primary path.

The CLI remains a stable human/debug and contract-validation interface, but NDJSON streaming output is out of scope for now.

## Why

- We already know the downstream consumer is MCP.
- In-process integration avoids subprocess overhead and stream parsing complexity.
- One retrieval implementation is maintained (no duplicated query logic).
- Existing JSON contract and golden tests remain useful for validation and regression checks.

## What This Means

1. Keep improving core query/index package APIs as the canonical behavior.
2. Keep CLI JSON output stable (`schemaVersion`) for testing and troubleshooting.
3. Do not add NDJSON unless MCP integration mode changes to subprocess streaming.

## Alternatives Considered

1. MCP via CLI subprocess + NDJSON streaming:
- Pros: earliest streaming interoperability over stdout.
- Cons: process orchestration and stream framing complexity; duplicate integration paths.

2. MCP via CLI subprocess + single JSON payload:
- Pros: simple to prototype.
- Cons: slower startup and less ergonomic for high-throughput internal calls.

3. MCP in-process package integration (chosen):
- Pros: fastest runtime path, strongest type-safety, easiest to evolve with tests.
- Cons: requires MCP server code to link and call internal API wrappers.

## Revisit Triggers

Revisit this decision only if one of the following happens:

- MCP must run confluence2md-indexer as an external process.
- A non-Go downstream consumer requires line-by-line streaming over stdout.
- Query result sizes/latency patterns require progressive transport from CLI.

## Operational Rule

Until a revisit trigger occurs:

- No NDJSON feature work.
- No dual retrieval implementations.
- MCP integration work targets package APIs first.
