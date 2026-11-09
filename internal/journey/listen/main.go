// listen is a journey's stand-in for a developer's server: it listens on the TCP port it is
// given, on 127.0.0.1, until it is stopped. Given "idle" instead, it listens on nothing and waits,
// as an editor or app opened from a session does. It is built beside engineroom for the journeys only.
package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: listen <port>")
		os.Exit(2)
	}
	if os.Args[1] == "idle" {
		for {
			time.Sleep(time.Hour)
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for {
		c, err := l.Accept()
		if err != nil {
			os.Exit(1)
		}
		_ = c.Close()
	}
}
