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
[2026-04-01 12:34:56 JST] 41.1 MB
```

Layout: `/<namespace>/<service>/{logs,metrics}/<signal-name>.dat`.
Commands: `ls`, `cd`, `cat`, `pwd`, `help`, `exit`.

## Try it with the sample app

`cmd/sample-app` is a fake web service built on the OpenTelemetry Go SDK.
It exports request and error counters, a latency histogram, in-flight
requests, a memory gauge, CPU utilization, and uptime every 5 seconds, plus
one log record per simulated request. Counters and the histogram use delta
temporality, so each sample is the count for that interval and threshold
alerts on them can resolve. Every minute or two a service "blips" for a few
seconds of elevated errors and traffic, enough to cross a threshold but not
to hold it.

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

Override `SAMPLE_NS`, `SAMPLE_SVC`, and `SAMPLE_RPS` to fake a different
service, e.g. `make run-sample-app SAMPLE_NS=web SAMPLE_SVC=frontend`. Or
list services in a config file and pass `SAMPLE_CONF=<file>`. Two are
checked in, meant to run in separate terminals:

```
make run-sample-app SAMPLE_CONF=sample-app.api.conf   # iam/iam-api
make run-sample-app SAMPLE_CONF=sample-app.web.conf   # iam/iam-web
```

A file may list several services, one `service <namespace> <name>
[rps=<n>]` per line, and a single process then simulates all of them.
`alert.sample-app.conf` holds matching alert rules; edit the webhook URL and
start the SoR with `make run-sor ALERTS=alert.sample-app.conf`.

`make run-webui` (needs Node 20+) serves the web dashboard on
http://localhost:8080; see [Web dashboard](#web-dashboard).

Press `b` in the sample app's terminal to fake an incident: for 3 minutes
(`-burst`) traffic goes 10x, half the requests fail, memory climbs to 512 MB
without a GC, and CPU pins above 0.9. The app prints `Bursting...` and `Ended burst`,
the memory and CPU rules fire, and each sends a resolved message once the
burst is over. With several services, `b` bursts all of them and `1`-`9`
burst one. `p` pauses and resumes the memory and CPU gauges, so their
streams go absent and the `absent` rules fire. Press `q` to quit.

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

A second form catches streams that stop arriving:

```
alert /iam/iam-api/metrics/process.cpu.utilization.dat absent for 30s to ops
```

It fires when no sample has been ingested for the duration (evaluated every
second, `-check-every`), and resolves on the next sample. A stream that
never reports at all fires one duration after the SoR starts.

Channels come in two kinds:

```
channel ops    slack     https://hooks.slack.com/services/T000/B000/XXXX
channel oncall slack-bot xoxb-your-bot-token C0123456789
```

A `slack` channel is an incoming webhook: every ALERT and every RESOLVED is
a new message. A `slack-bot` channel uses a bot token with the `chat:write`
scope and a channel ID; the ALERT is posted once and, when it resolves,
that same message is edited to read RESOLVED with both the detection and
the resolution time. Messages begin with `ALERT:<path>` or
`RESOLVED:<path>` so Slack keyword notifications can match on either.

## Web dashboard

`webui/` is a small [Hono](https://hono.dev) server that talks to the SoR
over gRPC (port 4319 by default, `-grpc` on `sor`) and renders a dashboard
of every metric that has an alert rule. Rules come from the SoR, so start
it with `-alerts`.

```
make run-sor ALERTS=alert.sample-app.conf   # terminal 1
make run-sample-app                         # terminal 2
make run-webui                              # terminal 3, then open http://localhost:8080
```

- `/` — tabular view: namespaces, with services collapsed. Expanding a
  service lists its KPIs with the latest value and a sparkline. Collapsed
  rows keep red (firing) and amber (over threshold) badges so nothing hides.
- `/heatmap`, `/heatmap/{namespace}`, `/heatmap/{namespace}/{service}` —
  a treemap in the style of a stock-market heatmap: namespaces contain
  services, services contain one tile per KPI. Amber when the latest value
  crosses a rule's threshold, red once the rule has held for its `for`
  duration and the alert has fired, green within thresholds, grey with no
  data. Tile area grows with value ÷ threshold so the KPIs closest to
  breaching stand out. Narrower paths zoom in.
- Clicking a KPI in either view opens its full history: every sample the
  SoR still holds, with each rule's threshold drawn on the chart.

All pages update live: the server holds one gRPC watch on the SoR and
relays samples and alert transitions to browsers over server-sent events.

The gRPC contract lives in `proto/otellite/v1/sor.proto`; `make proto`
regenerates both the Go server stubs and the TypeScript client.

