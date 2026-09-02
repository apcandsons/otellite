# OTel Lite

A deliberately minimal observability system for small projects. It accepts
OpenTelemetry metrics and logs over OTLP/HTTP, keeps them in memory for a
short window (default 3 hours), and lets you browse them from a shell that
feels like a filesystem. Nothing is persisted. Forgetting is a feature.

## Build

```
go build ./...
go test ./... -race
```

## Run the system of record

```
go run ./cmd/sor -listen :4318 -retention 3h -max-samples 1000000
```

Point any OTLP/HTTP exporter at `http://localhost:4318` (protobuf or JSON
encoding). Namespace and service come from the `service.namespace` and
`service.name` resource attributes; they default to `default` and
`unknown`. Log streams are named after the instrumentation scope.

## Browse

```
go run ./cmd/cli -sor http://localhost:4318
/> ls
iam/
/> cd iam/iam-api
/iam/iam-api> ls metrics
go.memory.used.dat
/iam/iam-api> cat metrics/go.memory.used.dat
[2026-04-01 12:34:56 JST] 43122688 Bytes
```

Layout: `/<namespace>/<service>/{logs,metrics}/<signal-name>.dat`.
Commands: `ls`, `cd`, `cat`, `pwd`, `help`, `exit`.

## Alerting

Start the SoR with `-alerts alert.conf`. The file declares channels and
rules, one per line:

```
# channel <name> slack <incoming-webhook-url>
channel ops slack https://hooks.slack.com/services/T000/B000/XXXX

# alert <metric path> <op> <threshold> for <duration> to <channel>
alert /iam/iam-api/metrics/go.memory.used.dat > 500000000 for 3m to ops
```

Operators: `>`, `>=`, `<`, `<=`. A rule fires once when the condition has
held continuously for the given duration, and re-arms after the metric
recovers. Only Slack incoming webhooks are supported.
