# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What OTel Lite is

A deliberately minimal observability system for small/"baby" projects. It accepts incoming OpenTelemetry metrics and logs, keeps them in memory for a short window, and exposes them through a CLI shell that is navigated like a filesystem. It is not meant to compete with full observability stacks; simplicity is the product.

## Core design constraints

- **Short retention window.** All data lives in memory for a configurable window (default 3 hours). Nothing is persisted across restarts.
- **Forgetting is a feature.** When memory runs out, the oldest data is dropped. Do not add eviction "safety nets", spill-to-disk, or persistence layers unless explicitly asked.
- **Two signal types:** `logs` and `metrics`, scoped per service.

## Data model (as exposed by the CLI)

The system of record (SoR) is presented as a virtual filesystem:

```
/<namespace>/<service>/{logs,metrics}/<signal-name>.dat
```

Example: `/iam/iam-api/metrics/go.memory.used.dat`

- Top level: namespaces/projects (e.g. `/iam`, `/gsm`, `/web`).
- Second level: services within a namespace (e.g. `iam-api`).
- Third level: fixed `logs` and `metrics` directories.
- Leaf: one `.dat` file per metric or log stream, named after the OTel signal name.

## CLI

The CLI connects to the SoR and supports shell-style navigation commands: `ls`, `cd`, `cat`. `cat` on a `.dat` file prints one sample per line, oldest first:

```
[2026-04-01 12:34:56 JST] 43122688 Bytes
```

Format: bracketed local timestamp with zone, value, unit. Keep new CLI commands consistent with this filesystem metaphor rather than adding query-language-style flags.

## Language and toolchain

Go, for both binaries: `sor` (the in-memory system of record + OTel ingest) and `cli` (the shell-style client). Single Go module, one `cmd/` entry per binary.

```
go build ./...                       # build everything
go test ./...                        # run all tests
go test ./internal/<pkg>/ -run TestName   # run a single test
go test ./... -race                  # run with race detector (SoR is concurrent)
go vet ./...                         # lint
```

## Development process

- **TDD, strictly Red/Green.** Write the failing test first, run it and confirm it fails for the right reason, then write the minimum code to pass, then refactor. Do not write production code without a failing test driving it.
- **Clean Architecture.** Dependencies point inward only: `domain` <- `usecase` <- `adapter` <- `cmd`. Domain and use-case packages must not import net, OTel SDKs, or CLI libraries. Outer layers depend on interfaces defined by inner layers.
- **SOLID.** In particular: keep interfaces small and defined at the consumer (interface segregation, dependency inversion); the retention/eviction policy, the store, the ingest transport, and the CLI transport are separate replaceable components (single responsibility, open/closed).

Suggested layout (adjust as the code grows, but keep the layering):

```
cmd/sor/            main for the system of record
cmd/cli/            main for the CLI
internal/domain/    entities: namespace, service, signal stream, sample; retention rules
internal/usecase/   ingest, evict, browse (ls/cd/cat) use cases
internal/adapter/   OTel receiver, in-memory store, CLI transport/protocol
```
