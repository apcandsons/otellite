package otlp_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"github.com/apcandsons/otellite/internal/adapter/otlp"
	"github.com/apcandsons/otellite/internal/domain"
)

type rec struct {
	id domain.StreamID
	s  domain.Sample
}

type sink struct{ got []rec }

func (s *sink) Ingest(id domain.StreamID, sm domain.Sample) { s.got = append(s.got, rec{id, sm}) }

var (
	t0   = time.Date(2026, 4, 1, 3, 34, 56, 0, time.UTC)
	nano = uint64(t0.UnixNano())
)

func resource(ns, name string) *resourcepb.Resource {
	var attrs []*commonpb.KeyValue
	if ns != "" {
		attrs = append(attrs, str("service.namespace", ns))
	}
	if name != "" {
		attrs = append(attrs, str("service.name", name))
	}
	return &resourcepb.Resource{Attributes: attrs}
}

func str(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func metricsReq() *colmetrics.ExportMetricsServiceRequest {
	return &colmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: resource("iam", "iam-api"),
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
			{Name: "go.memory.used", Unit: "By", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{
				{TimeUnixNano: nano, Value: &metricspb.NumberDataPoint_AsInt{AsInt: 43122688}},
			}}}},
			{Name: "http.requests", Unit: "1", Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: []*metricspb.NumberDataPoint{
				{TimeUnixNano: nano, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 12.5}},
			}}}},
			{Name: "http.duration", Unit: "ms", Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{DataPoints: []*metricspb.HistogramDataPoint{
				{TimeUnixNano: nano, Count: 3, Sum: proto.Float64(30)},
			}}}},
		}}},
	}}}
}

func post(t *testing.T, h http.Handler, path, ctype string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestMetricsProtobuf(t *testing.T) {
	sk := &sink{}
	h := otlp.NewHandler(sk, func() time.Time { return t0 })
	body, _ := proto.Marshal(metricsReq())
	w := post(t, h, "/v1/metrics", "application/x-protobuf", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if w.Header().Get("Content-Type") != "application/x-protobuf" {
		t.Errorf("response content type = %q", w.Header().Get("Content-Type"))
	}
	want := []rec{
		{domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "go.memory.used"}, domain.Sample{Time: t0, Value: "43122688", Unit: "By"}},
		{domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "http.requests"}, domain.Sample{Time: t0, Value: "12.5", Unit: "1"}},
		{domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "http.duration.count"}, domain.Sample{Time: t0, Value: "3", Unit: "1"}},
		{domain.StreamID{Namespace: "iam", Service: "iam-api", Kind: domain.Metrics, Name: "http.duration.sum"}, domain.Sample{Time: t0, Value: "30", Unit: "ms"}},
	}
	if len(sk.got) != len(want) {
		t.Fatalf("got %+v", sk.got)
	}
	for i := range want {
		if sk.got[i].id != want[i].id || !sk.got[i].s.Time.Equal(want[i].s.Time) || sk.got[i].s.Value != want[i].s.Value || sk.got[i].s.Unit != want[i].s.Unit {
			t.Errorf("[%d] got %+v want %+v", i, sk.got[i], want[i])
		}
	}
}

func TestMetricsJSON(t *testing.T) {
	sk := &sink{}
	h := otlp.NewHandler(sk, func() time.Time { return t0 })
	body, _ := protojson.Marshal(metricsReq())
	w := post(t, h, "/v1/metrics", "application/json", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("response content type = %q", w.Header().Get("Content-Type"))
	}
	if len(sk.got) != 4 {
		t.Fatalf("got %d samples", len(sk.got))
	}
}

func TestLogsDefaultsAndSeverity(t *testing.T) {
	sk := &sink{}
	h := otlp.NewHandler(sk, func() time.Time { return t0.Add(time.Minute) })
	req := &collogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: resource("", "web"),
		ScopeLogs: []*logspb.ScopeLogs{
			{Scope: &commonpb.InstrumentationScope{Name: "http"}, LogRecords: []*logspb.LogRecord{
				{TimeUnixNano: nano, SeverityText: "INFO", Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hello"}}},
			}},
			{LogRecords: []*logspb.LogRecord{
				{SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_ERROR, Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "boom"}}},
			}},
		},
	}}}
	body, _ := proto.Marshal(req)
	w := post(t, h, "/v1/logs", "application/x-protobuf", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(sk.got) != 2 {
		t.Fatalf("got %+v", sk.got)
	}
	first, second := sk.got[0], sk.got[1]
	if first.id != (domain.StreamID{Namespace: "default", Service: "web", Kind: domain.Logs, Name: "http"}) || first.s.Value != "INFO hello" || !first.s.Time.Equal(t0) {
		t.Errorf("first = %+v", first)
	}
	if second.id != (domain.StreamID{Namespace: "default", Service: "web", Kind: domain.Logs, Name: "default"}) || second.s.Value != "ERROR boom" || !second.s.Time.Equal(t0.Add(time.Minute)) {
		t.Errorf("second = %+v", second)
	}
}

func TestRejectsBadRequests(t *testing.T) {
	h := otlp.NewHandler(&sink{}, nil)
	if w := post(t, h, "/v1/metrics", "application/x-protobuf", []byte{0xff, 0xff}); w.Code != http.StatusBadRequest {
		t.Errorf("garbage proto: %d", w.Code)
	}
	if w := post(t, h, "/v1/metrics", "text/plain", nil); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("bad content type: %d", w.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/logs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", w.Code)
	}
}
