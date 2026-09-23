package cmd

import (
	"fmt"
	"net"
	"os"
	"testing"
)

// TestMain points XDG_STATE_HOME at a process-level temporary directory so
// that no test can touch the real ~/.local/state/ml tree: tests that skip
// fakeLogDir (e.g. argument-validation error paths) would otherwise append
// to the real ml-<port>.log of a locally running server, or conjure a
// phantom port into ml --status.
func TestMain(m *testing.M) {
	state, err := os.MkdirTemp("", "ml-cmd-test-state")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ml test: cannot create temp state dir: %v\n", err)
		os.Exit(1)
	}
	os.Setenv("XDG_STATE_HOME", state) //nolint:errcheck
	code := m.Run()
	os.RemoveAll(state) //nolint:errcheck
	os.Exit(code)
}

// freePort reserves an ephemeral port and releases it, for tests that must
// never reach a server that may be listening on the default port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcpAddr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener address is not a TCP address")
	}
	port := tcpAddr.Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
