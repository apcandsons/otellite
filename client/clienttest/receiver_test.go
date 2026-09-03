package clienttest_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/apcandsons/otellite/client/clienttest"
)

const metricsJSON = `{"resourceMetrics":[{"resource":{"attributes":[
{"key":"service.namespace","value":{"stringValue":"iam"}},
{"key":"service.name","value":{"stringValue":"iam-api"}}]},
"scopeMetrics":[{"metrics":[{"name":"x","unit":"1","gauge":{"dataPoints":[{"asInt":"7"}]}}]}]}]}`

func TestReceiverRecordsWhatTheRealHandlerIngests(t *testing.T) {
	rcv := clienttest.NewReceiver(t)
	resp, err := http.Post(rcv.URL()+"/v1/metrics", "application/json", strings.NewReader(metricsJSON))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	want := []string{"/iam/iam-api/metrics/x.dat"}
	if got := rcv.Streams(); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("Streams() = %v, want %v", got, want)
	}
	s := rcv.Samples(want[0])
	if len(s) != 1 || s[0].Value != "7" || s[0].Unit != "1" {
		t.Fatalf("Samples() = %+v", s)
	}
	rcv.Reset()
	if got := rcv.Streams(); len(got) != 0 {
		t.Fatalf("after Reset: %v", got)
	}
}
