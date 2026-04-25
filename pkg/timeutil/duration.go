package timeutil

import (
	"fmt"
	"time"
)

func HumanDuration(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	return translateDuration(time.Since(t))
}

func translateDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d < 30*24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if d < 365*24*time.Hour {
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
}
