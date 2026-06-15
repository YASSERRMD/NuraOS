package sockact

import "os"

// fileCloser wraps *os.File so tests and callers can use it without importing os.
type fileCloser struct {
	*os.File
}
