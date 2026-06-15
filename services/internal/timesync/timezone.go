package timesync

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const defaultTZFile = "/data/etc/timezone"

// LoadTimezone reads a timezone name (e.g. "America/New_York") from file and
// returns the corresponding *time.Location. Returns time.UTC when the file is
// absent (not an error: the system just defaults to UTC).
func LoadTimezone(file string) (*time.Location, error) {
	if file == "" {
		file = defaultTZFile
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return time.UTC, nil
	}
	tz := strings.TrimSpace(string(data))
	if tz == "" || tz == "UTC" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC, fmt.Errorf("unknown timezone %q: %w", tz, err)
	}
	return loc, nil
}

// ApplyTimezone installs loc as the process-wide local timezone by setting
// the TZ environment variable and updating time.Local.
func ApplyTimezone(loc *time.Location) {
	if loc == nil {
		loc = time.UTC
	}
	os.Setenv("TZ", loc.String())
	time.Local = loc
}
