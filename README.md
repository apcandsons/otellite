# OTel Lite

A deliberately minimal observability system for small projects. It accepts
OpenTelemetry metrics and logs over OTLP/HTTP, keeps them in memory for a
short window (default 3 hours), and lets you browse them from a shell that
feels like a filesystem. Nothing is persisted. Forgetting is a feature.

## Build

```
make build        # bin/sor, bin/cli, bin/sample-app
make test-race    # go test ./... -race
make check        # fmt + vet + race tests
make help         # all targets
```

## Run the system of record

```
make run-sor                                  # :4318, 3h retention, 1M samples
make run-sor LISTEN=:9999 RETENTION=1h MAX_SAMPLES=100000
```

Point any OTLP/HTTP exporter at `http://localhost:4318` (protobuf or JSON
encoding). Namespace and service come from the `service.namespace` and
`service.name` resource attributes; they default to `default` and
`unknown`. Log streams are named after the instrumentation scope.

## Browse

```
make run-cli                                  # SOR_URL=http://localhost:4318
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

## Try it with the sample app

`cmd/sample-app` is a fake web service built on the OpenTelemetry Go SDK.
It exports request counters, a latency histogram, in-flight requests, a
memory gauge, CPU utilization, and uptime every 5 seconds, plus one log
record per simulated request.

```
make run-sor                     # terminal 1
make run-sample-app              # terminal 2
make run-cli                     # terminal 3
/> cd iam/iam-api
/iam/iam-api> ls metrics
go.memory.used.dat
http.server.active_requests.dat
http.server.duration.count.dat
http.server.duration.sum.dat
http.server.errors.dat
http.server.requests.dat
process.cpu.utilization.dat
process.uptime.dat
```

Override `SAMPLE_NS`, `SAMPLE_SVC`, and `SAMPLE_RPS` to fake more
services, e.g. `make run-sample-app SAMPLE_NS=web SAMPLE_SVC=frontend`.
`alert.sample-app.conf` holds matching alert rules; edit the webhook URL and
start the SoR with `make run-sor ALERTS=alert.sample-app.conf`.

Press `b` in the sample app's terminal to fake an incident: for 3 minutes
(`-burst`) traffic goes 10x, half the requests fail, memory climbs to 512 MB
without a GC, and CPU pins above 0.9. The app prints `Bursting...` and `Ended burst`,
the memory and CPU rules fire, and each sends a resolved message once the
burst is over. Press `q` to quit.

## Alerting

Start the SoR with `make run-sor ALERTS=alert.conf`. The file declares
channels and rules, one per line:

```
# channel <name> slack <incoming-webhook-url>
channel ops slack https://hooks.slack.com/services/T000/B000/XXXX

# alert <metric path> <op> <threshold> for <duration> to <channel>
alert /iam/iam-api/metrics/go.memory.used.dat > 500000000 for 3m to ops
```

Operators: `>`, `>=`, `<`, `<=`. A rule fires once when the condition has
held continuously for the given duration. When the metric recovers, a
"resolved" message is sent to the same channel and the rule re-arms. Only
Slack incoming webhooks are supported.
