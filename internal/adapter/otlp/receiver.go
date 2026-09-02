// Package otlp receives OpenTelemetry metrics and logs over OTLP/HTTP
// (protobuf or JSON) and forwards them to the ingest use case.
package otlp

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"github.com/apcandsons/otellite/internal/domain"
)

// Sink is where decoded samples go.
type Sink interface {
	Ingest(domain.StreamID, domain.Sample)
}

const (
	ctProto = "application/x-protobuf"
	ctJSON  = "application/json"

	defaultNamespace = "default"
	defaultService   = "unknown"
	defaultLogStream = "default"

	maxBody = 16 << 20
)

// NewHandler returns an http.Handler serving /v1/metrics and /v1/logs.
func NewHandler(sink Sink, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}
	r := &receiver{sink: sink, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/metrics", func(w http.ResponseWriter, req *http.Request) {
		var in colmetrics.ExportMetricsServiceRequest
		r.serve(w, req, &in, &colmetrics.ExportMetricsServiceResponse{}, func() { r.metrics(&in) })
	})
	mux.HandleFunc("/v1/logs", func(w http.ResponseWriter, req *http.Request) {
		var in collogs.ExportLogsServiceRequest
		r.serve(w, req, &in, &collogs.ExportLogsServiceResponse{}, func() { r.logs(&in) })
	})
	return mux
}

type receiver struct {
	sink Sink
	now  func() time.Time
}

func (r *receiver) serve(w http.ResponseWriter, req *http.Request, in, out proto.Message, handle func()) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctype := req.Header.Get("Content-Type")
	if i := strings.IndexByte(ctype, ';'); i >= 0 {
		ctype = strings.TrimSpace(ctype[:i])
	}
	var unmarshal func([]byte, proto.Message) error
	var marshal func(proto.Message) ([]byte, error)
	switch ctype {
	case ctProto:
		unmarshal, marshal = proto.Unmarshal, proto.Marshal
	case ctJSON:
		unmarshal = protojson.Unmarshal
		marshal = func(m proto.Message) ([]byte, error) { return protojson.Marshal(m) }
	default:
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := unmarshal(body, in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	handle()
	resp, _ := marshal(out)
	w.Header().Set("Content-Type", ctype)
	w.Write(resp)
}

func (r *receiver) metrics(in *colmetrics.ExportMetricsServiceRequest) {
	for _, rm := range in.GetResourceMetrics() {
		ns, svc := identity(rm.GetResource())
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				r.metric(ns, svc, m)
			}
		}
	}
}

func (r *receiver) metric(ns, svc string, m *metricspb.Metric) {
	id := func(name string) domain.StreamID {
		return domain.StreamID{Namespace: ns, Service: svc, Kind: domain.Metrics, Name: name}
	}
	switch d := m.GetData().(type) {
	case *metricspb.Metric_Gauge:
		r.numbers(id(m.GetName()), m.GetUnit(), d.Gauge.GetDataPoints())
	case *metricspb.Metric_Sum:
		r.numbers(id(m.GetName()), m.GetUnit(), d.Sum.GetDataPoints())
	case *metricspb.Metric_Histogram:
		for _, dp := range d.Histogram.GetDataPoints() {
			ts := r.stamp(dp.GetTimeUnixNano())
			r.sink.Ingest(id(m.GetName()+".count"), domain.Sample{Time: ts, Value: strconv.FormatUint(dp.GetCount(), 10), Unit: "1"})
			if dp.Sum != nil {
				r.sink.Ingest(id(m.GetName()+".sum"), domain.Sample{Time: ts, Value: formatFloat(dp.GetSum()), Unit: m.GetUnit()})
			}
		}
	}
}

func (r *receiver) numbers(id domain.StreamID, unit string, dps []*metricspb.NumberDataPoint) {
	for _, dp := range dps {
		var value string
		switch v := dp.GetValue().(type) {
		case *metricspb.NumberDataPoint_AsInt:
			value = strconv.FormatInt(v.AsInt, 10)
		case *metricspb.NumberDataPoint_AsDouble:
			value = formatFloat(v.AsDouble)
		default:
			continue
		}
		r.sink.Ingest(id, domain.Sample{Time: r.stamp(dp.GetTimeUnixNano()), Value: value, Unit: unit})
	}
}

func (r *receiver) logs(in *collogs.ExportLogsServiceRequest) {
	for _, rl := range in.GetResourceLogs() {
		ns, svc := identity(rl.GetResource())
		for _, sl := range rl.GetScopeLogs() {
			name := sl.GetScope().GetName()
			if name == "" {
				name = defaultLogStream
			}
			id := domain.StreamID{Namespace: ns, Service: svc, Kind: domain.Logs, Name: name}
			for _, lr := range sl.GetLogRecords() {
				r.sink.Ingest(id, domain.Sample{Time: r.logStamp(lr), Value: severity(lr) + " " + anyValue(lr.GetBody())})
			}
		}
	}
}

func (r *receiver) stamp(nanos uint64) time.Time {
	if nanos == 0 {
		return r.now()
	}
	return time.Unix(0, int64(nanos)).UTC()
}

func (r *receiver) logStamp(lr *logspb.LogRecord) time.Time {
	if lr.GetTimeUnixNano() != 0 {
		return r.stamp(lr.GetTimeUnixNano())
	}
	return r.stamp(lr.GetObservedTimeUnixNano())
}

func identity(res *resourcepb.Resource) (ns, svc string) {
	ns, svc = defaultNamespace, defaultService
	for _, kv := range res.GetAttributes() {
		switch kv.GetKey() {
		case "service.namespace":
			ns = kv.GetValue().GetStringValue()
		case "service.name":
			svc = kv.GetValue().GetStringValue()
		}
	}
	return ns, svc
}

func severity(lr *logspb.LogRecord) string {
	if s := lr.GetSeverityText(); s != "" {
		return s
	}
	n := lr.GetSeverityNumber()
	switch {
	case n == 0:
		return "UNSET"
	case n <= 4:
		return "TRACE"
	case n <= 8:
		return "DEBUG"
	case n <= 12:
		return "INFO"
	case n <= 16:
		return "WARN"
	case n <= 20:
		return "ERROR"
	default:
		return "FATAL"
	}
}

func anyValue(v *commonpb.AnyValue) string {
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(x.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		return formatFloat(x.DoubleValue)
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(x.BoolValue)
	case nil:
		return ""
	default:
		b, _ := protojson.Marshal(v)
		return string(b)
	}
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
