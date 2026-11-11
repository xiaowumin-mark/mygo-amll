package ttml

import (
	"time"
)

func (d *documentParser) parseTime(value string) (time.Duration, error) {
	if d.appleWords && value != "" {
		bare := true
		for _, r := range value {
			if (r < '0' || r > '9') && r != '.' {
				bare = false
				break
			}
		}
		if bare {
			return decimalDuration(value, time.Second)
		}
	}
	return parseTime(value)
}
