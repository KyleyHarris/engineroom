package netport

import (
	"net"
	"strings"
	"testing"
)

// The real check, against a real listener: a port this test holds is taken and named; the same
// port once released is free. It fails if a bound port reads as free, which would leave ssh to
// fail the forward without the developer being told.
func TestPortTakenSeesARealListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if why := Taken(port); !strings.Contains(why, "already in use on this machine by") {
		t.Fatalf("a held port reads as %q", why)
	}
	_ = l.Close()
	if why := Taken(port); why != "" {
		t.Fatalf("a released port reads as %q", why)
	}
}

// Answers connects: a port with a server accepting on it answers, the same port once the server is
// gone does not. It fails if a running server reads as silent, which would start an app twice, or
// a closed port as answering, which would open a link to nothing.
func TestAnswersSeesARealServer(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if !Answers(port) {
		t.Fatal("a listening port does not answer")
	}
	_ = l.Close()
	if Answers(port) {
		t.Fatal("a closed port answers")
	}
}
