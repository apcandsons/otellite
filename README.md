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

## Instrumenting a Go service

`github.com/apcandsons/otellite/client` is the bootstrap for real
services: one call wires metrics and logs to the SoR from the standard
OpenTelemetry environment, and the rest of the package is thin helpers.

```go
import "github.com/apcandsons/otellite/client"

func main() {
    ctx := context.Background()
    shutdown, err := client.Start(ctx, client.Options{}) // reads OTEL_* below
    if err != nil {
        log.Fatal(err)
    }
    defer shutdown(ctx)

    denies, _ := client.NewCounterSet("authz.deny", "no_grant", "explicit_deny")
    denies.Add(ctx, "no_grant", 1)
    client.Gauge("sync.lag", "1", "sequence gap to the source", func() float64 { return lag() })

    http.ListenAndServe(":8080", client.HTTPMiddleware(mux))
    // or: grpc.NewServer(grpc.ChainUnaryInterceptor(client.UnaryServerInterceptor()))
}
```

The contract is environment only, so the same binary runs anywhere:

```
OTEL_EXPORTER_OTLP_ENDPOINT=http://sor.otel.staging.internal:4318   # unset: telemetry off
OTEL_SERVICE_NAME=iam-api                                            # /<namespace>/iam-api/...
OTEL_RESOURCE_ATTRIBUTES=service.namespace=iam,deployment.environment=staging
OTEL_METRIC_EXPORT_INTERVAL=60000                                    # ms, default 60 s
```

Without an endpoint `Start` only installs the slog fan-out (secrets still
redacted) and returns a no-op shutdown. With one, every service gets the
common set for free: `process.cpu.utilization`, `go.memory.used`,
`go.goroutines`, `process.uptime` (a heartbeat for `absent` rules), plus
`http.server.{requests,errors,duration,active_requests,bytes_in,bytes_out}`
from `HTTPMiddleware` and `rpc.server.{requests,errors,client_errors,
duration,active}` from the gRPC interceptors. `UnaryClientInterceptor("mkms")`
reports an outbound hop as `mkms.{requests,errors,duration}`.

Counters and histograms export with delta temporality every 60 s, so a
counter sample is literally "events in the last minute" and a threshold
alert on it resolves once the trouble passes. Histograms surface as
`<name>.count` and `<name>.sum`.

The SoR keys streams by instrument name only and drops attributes, so
never put a dimension in an attribute. Give each value its own instrument
instead, which is what `NewCounterSet` is for: `authz.deny.no_grant.dat`
and `authz.deny.explicit_deny.dat` are two files you can `cat` and alert
on; `authz.deny{reason=...}` would be one file with the reasons lost.

Logs: `Start` replaces slog's default with a fan-out to your own handler
and an OTLP bridge, and routes the standard `log` package through it at
INFO. Values whose key (or `group.key` path) matches `Options.Redact`
(default `password|secret|token|authorization|cookie|key`) become
`[REDACTED]` on both sinks. The SoR keeps severity and body only, so
attributes are rendered into the body as `msg k=v k=v`. The log stream is
named after the scope (`Options.Scope`, else `OTEL_SERVICE_NAME`) with `/`
replaced by `-`, since stream names are path segments:
`/iam/iam-api/logs/iam-api.dat`.

For a wiring test, `client/clienttest` starts the real receiver on an
`httptest` server:

```go
rcv := clienttest.NewReceiver(t)
shutdown, _ := client.Start(ctx, client.Options{Endpoint: rcv.URL(), Interval: 20 * time.Millisecond})
// ... exercise the code, then
shutdown(ctx)
rcv.Streams() // ["/iam/iam-api/metrics/authz.deny.no_grant.dat", ...]
```

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

Webhook URLs and bot tokens are secrets, so keep them out of the file:
`${NAME}` anywhere on a channel or alert line is replaced by the
environment variable `NAME` when the file is loaded. Only the `${...}`
form expands; a bare `$NAME` is left as written, and comment lines are
never touched. A reference to an unset variable is an error naming the
line: `alert.conf line 3: ${SLACK_WEBHOOK_URL} is not set`.

```
channel ops slack ${SLACK_WEBHOOK_URL}
alert /${NAMESPACE}/iam-api/metrics/go.memory.used.dat > 500000000 for 3m to ops
```

`sor -validate -alerts alert.conf` (or `make validate ALERTS=alert.conf`)
parses the file with the same expansion, prints `N rules, M channels`, and
exits 0 without starting any listener; a parse error or an unset variable
prints the line and exits 1. Run it in an image build so a bad rules file
fails the build, not the deploy. The build has no real secrets, so give
every reference a placeholder:

```
SLACK_WEBHOOK_URL=dummy sor -validate -alerts alert.conf
```

## Health check

`sor -healthcheck` sends `GET http://127.0.0.1:<port>/fs/ls?path=/` to a
running SoR, where the port comes from `-listen` (`:4318` and `host:port`
both work; an explicit non-wildcard host is kept), waits at most 2 s, and
exits 0 on HTTP 200 and 1 on anything else. It starts no listeners, so it
is the container health command for the same image:

```
"healthCheck": {"command": ["CMD", "/sor", "-healthcheck"]}   # ECS
HEALTHCHECK CMD ["/sor", "-healthcheck"]                        # Dockerfile
```

Pass the same `-listen` as the server when it is not the default.

## Docker

`sor` is a static Go binary with no runtime dependencies, and both the
build-time check (`-validate`) and the health probe (`-healthcheck`) are
flags on that binary. The image therefore needs no shell, no `curl`, and
no `envsubst`; `gcr.io/distroless/static-debian12:nonroot` is enough:

```dockerfile
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /sor ./cmd/sor
RUN SLACK_WEBHOOK_URL=dummy /sor -validate -alerts alert.conf

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /sor /sor
COPY alert.conf /alert.conf
HEALTHCHECK CMD ["/sor", "-healthcheck"]
ENTRYPOINT ["/sor", "-alerts", "/alert.conf"]
```

The real `SLACK_WEBHOOK_URL` arrives as a container environment variable
(an ECS secret, for instance) and is expanded when `sor` starts.

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

### Running behind a reverse proxy

The dashboard can mount under a prefix and gate itself, for when the
proxy in front of it is public:

```
BASE_PATH=/otellite WEBUI_TOKEN=$(openssl rand -base64 32) make run-webui
```

- `BASE_PATH` (default empty) mounts every route under the prefix, so the
  proxy forwards `/otellite/*` unchanged. Pages, `app.js`, the SSE feed
  and the fragments the pages fetch (`/_/detail/*`, `/_/history/*`) all
  live under it. `GET <base>/healthz` answers `ok` without a session.
- `WEBUI_TOKEN` (default empty) turns on login. Empty means no
  authentication at all, and the server says so loudly at start. With a
  token set, a browser without a session is redirected to
  `<base>/login`, where the token is exchanged for an `otellite_session`
  cookie (`HttpOnly; SameSite=Lax; Path=<base>; Secure; Max-Age=30d`,
  holding an HMAC of the token, not the token). Scripts and probes can
  send `Authorization: Bearer <token>` instead. Anything else without
  credentials gets a 401.
- `COOKIE_SECURE=false` drops the `Secure` attribute for plain-http
  development.

