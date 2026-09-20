# Support Matrix (Draft)

Policy:

- The project ships one standalone executable per supported platform.
- Vector capability is mandatory and must work offline: the default provider (`bow-local`) needs no network, no model download and no API key, so every target can index and search vectors out of the box. Hosted providers are optional extras.

## Target matrix

| OS | Arch | Status | Notes |
| --- | --- | --- | --- |
| linux | amd64 | draft | Release candidate target |
| windows | amd64 | draft | Release candidate target |
| darwin | arm64 | draft | Release candidate target |

## Release gate checks per target

1. Binary builds successfully.
2. Binary starts and executes CLI help.
3. Vector smoke gate passes: index a fixture corpus with the default provider and query it with `--mode vector`, expecting results.
4. End-to-end index/query/stats JSON contract smoke (golden tests in CI).

The vector smoke gate is `task smoke:vector` (`go run ./internal/tools/smokevector`).
It indexes a fixture corpus with the default provider and checks the vector query,
hybrid fusion, lexical-only retrieval and the embedding identity guard, all without
network access, and runs in CI on every push and pull request.
