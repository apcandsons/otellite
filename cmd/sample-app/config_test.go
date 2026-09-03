package main

import (
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	in := `
# services to simulate
service iam iam-api rps=20
service iam iam-web          # default rps
service web frontend rps=2.5
`
	got, err := parseConfig(strings.NewReader(in), 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []serviceConfig{
		{Namespace: "iam", Service: "iam-api", RPS: 20},
		{Namespace: "iam", Service: "iam-web", RPS: 10},
		{Namespace: "web", Service: "frontend", RPS: 2.5},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseConfigErrors(t *testing.T) {
	for _, in := range []string{
		"service iam",                              // missing service name
		"service iam iam-api rps=fast",             // bad rps
		"service iam iam-api rps=0",                // rps must be positive
		"service iam iam-api burst=1",              // unknown option
		"servce iam iam-api",                       // unknown directive
		"service iam iam-api\nservice iam iam-api", // duplicate
		"service a/b c",                            // slash would break the stream path
		"",                                         // no services at all
	} {
		if _, err := parseConfig(strings.NewReader(in), 10); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}
