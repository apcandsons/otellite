package domain

import (
	"fmt"
	"strconv"
)

// IsBytes reports whether a unit string means bytes ("By" is the OTel/UCUM
// code, "Bytes" appears in older exporters).
func IsBytes(unit string) bool { return unit == "By" || unit == "Bytes" }

// Display renders a sample value for people: byte counts become KB / MB /
// GB (base 1024, one decimal), other non-integer numbers are rounded to
// 4 significant digits, the OTel dimensionless unit "1" is dropped, and
// anything else is "value unit".
func Display(value, unit string) string {
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		switch {
		case IsBytes(unit):
			return Bytes(n)
		case n != float64(int64(n)):
			value = strconv.FormatFloat(n, 'g', 4, 64)
		}
	}
	if unit == "" || unit == "1" {
		return value
	}
	return value + " " + unit
}

// Bytes formats a byte count as B, KB, MB, GB, or TB.
func Bytes(n float64) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	if abs < 1024 {
		return fmt.Sprintf("%.0f B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := n / 1024
	i := 0
	for ; i < len(units)-1 && (v >= 1024 || v <= -1024); i++ {
		v /= 1024
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
