# Support Matrix (Draft)

Policy:

- The project ships one standalone executable per supported platform.
- Vector capability is mandatory. If vector capability does not pass, that target is not supported for release.

## Target matrix

| OS | Arch | Status | Notes |
| --- | --- | --- | --- |
| linux | amd64 | draft | Release candidate target |
| windows | amd64 | draft | Release candidate target |
| darwin | arm64 | draft | Release candidate target |

## Release gate checks per target

1. Binary builds successfully.
2. Binary starts and executes CLI help.
3. Vector smoke gate passes.
4. End-to-end index/query/stats JSON contract smoke (golden tests in CI).
