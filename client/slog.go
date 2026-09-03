package client

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
)

const redacted = "[REDACTED]"

// fanout delivers each record to every sink after redacting attribute
// values whose key path matches the pattern. The first sink is the app's
// handler and its level governs the whole fan-out, so a service that logs
// at INFO does not flood the SoR with DEBUG.
type fanout struct {
	redact *regexp.Regexp
	sinks  []slog.Handler
	prefix string // open groups, as "a.b."
}

func newFanout(redact *regexp.Regexp, sinks ...slog.Handler) *fanout {
	return &fanout{redact: redact, sinks: sinks}
}

func (f *fanout) Enabled(ctx context.Context, l slog.Level) bool {
	return f.sinks[0].Enabled(ctx, l)
}

func (f *fanout) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(f.redact, f.prefix, a))
		return true
	})
	var errs []error
	for _, s := range f.sinks {
		if s.Enabled(ctx, r.Level) {
			errs = append(errs, s.Handle(ctx, clean))
		}
	}
	return errors.Join(errs...)
}

func (f *fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		clean[i] = redactAttr(f.redact, f.prefix, a)
	}
	return f.derive(f.prefix, func(s slog.Handler) slog.Handler { return s.WithAttrs(clean) })
}

func (f *fanout) WithGroup(name string) slog.Handler {
	if name == "" {
		return f
	}
	return f.derive(f.prefix+name+".", func(s slog.Handler) slog.Handler { return s.WithGroup(name) })
}

func (f *fanout) derive(prefix string, fn func(slog.Handler) slog.Handler) *fanout {
	sinks := make([]slog.Handler, len(f.sinks))
	for i, s := range f.sinks {
		sinks[i] = fn(s)
	}
	return &fanout{redact: f.redact, sinks: sinks, prefix: prefix}
}

// redactAttr replaces the value of a when prefix+key matches re, and
// recurses into groups so nested keys are matched by their dotted path.
func redactAttr(re *regexp.Regexp, prefix string, a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		sub := prefix
		if a.Key != "" {
			sub += a.Key + "."
		}
		members := a.Value.Group()
		clean := make([]slog.Attr, len(members))
		for i, m := range members {
			clean[i] = redactAttr(re, sub, m)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(clean...)}
	}
	if re.MatchString(prefix + a.Key) {
		return slog.String(a.Key, redacted)
	}
	return a
}

// bridge is the slog.Handler that emits OTel log records. The SoR keeps
// only severity and body, so attributes are rendered into the body.
type bridge struct {
	logger otellog.Logger
	prefix string // open groups, as "a.b."
	attrs  string // pre-rendered " k=v" pairs from WithAttrs
}

func newBridge(l otellog.Logger) *bridge { return &bridge{logger: l} }

func (b *bridge) Enabled(context.Context, slog.Level) bool { return true }

func (b *bridge) Handle(ctx context.Context, r slog.Record) error {
	var body strings.Builder
	body.WriteString(r.Message)
	body.WriteString(b.attrs)
	r.Attrs(func(a slog.Attr) bool {
		renderAttr(&body, b.prefix, a)
		return true
	})

	var rec otellog.Record
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	rec.SetTimestamp(ts)
	rec.SetObservedTimestamp(time.Now())
	sev, text := severity(r.Level)
	rec.SetSeverity(sev)
	rec.SetSeverityText(text)
	rec.SetBody(attribute.StringValue(body.String()))
	b.logger.Emit(ctx, rec)
	return nil
}

func (b *bridge) WithAttrs(attrs []slog.Attr) slog.Handler {
	var sb strings.Builder
	for _, a := range attrs {
		renderAttr(&sb, b.prefix, a)
	}
	return &bridge{logger: b.logger, prefix: b.prefix, attrs: b.attrs + sb.String()}
}

func (b *bridge) WithGroup(name string) slog.Handler {
	if name == "" {
		return b
	}
	return &bridge{logger: b.logger, prefix: b.prefix + name + ".", attrs: b.attrs}
}

// renderAttr appends " key=value", flattening groups into dotted keys.
func renderAttr(sb *strings.Builder, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		sub := prefix
		if a.Key != "" {
			sub += a.Key + "."
		}
		for _, m := range a.Value.Group() {
			renderAttr(sb, sub, m)
		}
		return
	}
	sb.WriteString(" ")
	sb.WriteString(prefix + a.Key)
	sb.WriteString("=")
	sb.WriteString(renderValue(a.Value))
}

func renderValue(v slog.Value) string {
	s := v.String()
	if s == "" || strings.ContainsAny(s, " =\"\n\t\r") {
		return strconv.Quote(s)
	}
	return s
}

// severity maps slog levels (and the gaps between them) to OTel severities.
func severity(l slog.Level) (otellog.Severity, string) {
	switch {
	case l < slog.LevelDebug:
		return otellog.SeverityTrace, "TRACE"
	case l < slog.LevelInfo:
		return otellog.SeverityDebug, "DEBUG"
	case l < slog.LevelWarn:
		return otellog.SeverityInfo, "INFO"
	case l < slog.LevelError:
		return otellog.SeverityWarn, "WARN"
	case l < slog.LevelError+4:
		return otellog.SeverityError, "ERROR"
	default:
		return otellog.SeverityFatal, "FATAL"
	}
}
