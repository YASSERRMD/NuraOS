package harness

import "net"

// freePort returns a free TCP port on localhost by binding to :0 and
// immediately releasing the listener. There is a small TOCTOU window
// between releasing and QEMU binding, which is acceptable for test use.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}
