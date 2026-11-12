package collector

import "time"

func timeDurationMs(ms int) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
