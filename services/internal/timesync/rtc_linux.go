//go:build linux

package timesync

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// rtcTime mirrors struct rtc_time from <linux/rtc.h>.
type rtcTime struct {
	Sec   int32
	Min   int32
	Hour  int32
	Mday  int32
	Mon   int32
	Year  int32
	Wday  int32
	Yday  int32
	Isdst int32
}

// rtcRdTime is the ioctl number for RTC_RD_TIME on Linux x86-64.
const rtcRdTime uintptr = 0x80247009

// ReadRTC opens device (default /dev/rtc0) and reads the hardware clock time
// in UTC. On failure it returns time.Now() and a non-nil error so callers can
// fall back to the current system time.
func ReadRTC(device string) (time.Time, error) {
	if device == "" {
		device = "/dev/rtc0"
	}
	f, err := os.Open(device)
	if err != nil {
		return time.Now(), fmt.Errorf("open rtc: %w", err)
	}
	defer f.Close()

	var rt rtcTime
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL,
		f.Fd(), rtcRdTime, uintptr(unsafe.Pointer(&rt)))
	if errno != 0 {
		return time.Now(), fmt.Errorf("RTC_RD_TIME ioctl: %w", errno)
	}

	t := time.Date(int(rt.Year)+1900, time.Month(rt.Mon+1), int(rt.Mday),
		int(rt.Hour), int(rt.Min), int(rt.Sec), 0, time.UTC)
	return t, nil
}

// SetSystemTime calls settimeofday to adjust the system clock to t.
// Requires CAP_SYS_TIME; errors are non-fatal (caller logs and continues).
func SetSystemTime(t time.Time) error {
	tv := syscall.Timeval{
		Sec:  t.Unix(),
		Usec: int64(t.Nanosecond() / 1000),
	}
	return syscall.Settimeofday(&tv)
}
