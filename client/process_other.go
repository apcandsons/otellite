//go:build !unix

package client

import "time"

// cpuTime is unavailable here; process.cpu.utilization reports 0.
func cpuTime() (time.Duration, bool) { return 0, false }
