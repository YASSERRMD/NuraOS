//go:build !linux

package timesync

import "time"

// ReadRTC returns the current system time on non-Linux platforms.
// The device argument is ignored.
func ReadRTC(device string) (time.Time, error) { return time.Now(), nil }

// SetSystemTime is a no-op on non-Linux platforms.
func SetSystemTime(t time.Time) error { return nil }
