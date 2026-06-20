package apistate

import "time"

func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Second {
		return "0s"
	}
	return d.Truncate(100 * time.Millisecond).String()
}
