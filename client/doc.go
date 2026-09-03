// Package client is the OpenTelemetry bootstrap for Go services that report
// to an OTel Lite system of record. One call to Start wires metrics and
// logs; the rest of the package is thin helpers over the global meter and
// the process-wide slog logger.
//
// # Contract
//
// Identity and destination come from the standard environment variables,
// never from code, so the same binary runs anywhere:
//
//	OTEL_EXPORTER_OTLP_ENDPOINT   http://sor.otel.staging.internal:4318 (unset: telemetry off)
//	OTEL_SERVICE_NAME             iam-api            -> /<namespace>/iam-api/...
//	OTEL_RESOURCE_ATTRIBUTES      service.namespace=iam,deployment.environment=staging
//	OTEL_METRIC_EXPORT_INTERVAL   60000 (milliseconds; default 60 s)
//
// With no endpoint, Start only installs the slog fan-out (with redaction)
// and returns a no-op shutdown, so local runs need no configuration.
//
// # Metrics
//
// Counters and histograms export with delta temporality every 60 s, so
// each sample of a counter is literally "events in the last minute"; there
// is no rate helper because none is needed. Everything else is cumulative.
// OTel Lite keys streams by instrument name only and drops attributes, so
// never put a dimension in an attribute: give each value its own
// instrument, which is what NewCounterSet is for. Histograms surface as
// <name>.count and <name>.sum.
//
// Call Start before creating instruments. Instruments made earlier bind to
// the SDK's global delegate and still work, but the process gauges and the
// HTTP/gRPC instruments are created lazily from Meter after Start.
//
// # Logs
//
// Start replaces slog's default logger with a fan-out to the app's own
// handler and an OTLP bridge, and points the standard log package at it.
// Values whose key (or group.key path) matches Options.Redact are replaced
// with "[REDACTED]" on both sinks. The SoR keeps only severity and body, so
// the bridge renders attributes into the body as "msg k=v k=v". The log
// stream is named after the scope (Options.Scope, else OTEL_SERVICE_NAME)
// with "/" replaced by "-", because stream names are path segments.
package client
