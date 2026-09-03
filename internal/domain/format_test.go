package domain_test

import (
	"testing"

	"github.com/apcandsons/otellite/internal/domain"
)

func TestDisplayHumanizesBytes(t *testing.T) {
	cases := map[[2]string]string{
		{"43122688", "By"}:     "41.1 MB",
		{"43122688", "Bytes"}:  "41.1 MB",
		{"120000000", "By"}:    "114.4 MB",
		{"512", "By"}:          "512 B",
		{"1024", "By"}:         "1.0 KB",
		{"5368709120", "By"}:   "5.0 GB",
		{"0", "By"}:            "0 B",
		{"-2048", "By"}:        "-2.0 KB",
		{"1.5", "s"}:           "1.5 s",
		{"7882", "1"}:          "7882",
		{"INFO started", ""}:   "INFO started",
		{"not-a-number", "By"}: "not-a-number By",
	}
	for in, want := range cases {
		if got := domain.Display(in[0], in[1]); got != want {
			t.Errorf("Display(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
	if got := domain.IsBytes("By"); !got {
		t.Error("By should be bytes")
	}
	if got := domain.IsBytes("ms"); got {
		t.Error("ms should not be bytes")
	}
}

func TestDisplayRoundsLongFloats(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"0.44895942980742365", "1"}: "0.449",
		{"9065.25", "s"}:             "9065 s",
		{"0.95086937", "1"}:          "0.9509",
		{"12", "s"}:                  "12 s",
	} {
		if got := domain.Display(in[0], in[1]); got != want {
			t.Errorf("Display(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
