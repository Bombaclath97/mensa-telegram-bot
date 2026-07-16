package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// healthcheckTimeout bounds the probe. A hung /health is a failed /health.
const healthcheckTimeout = 3 * time.Second

// runHealthcheck probes this process's own /health endpoint and returns a process exit code.
//
// # Why the binary probes itself
//
// The runtime image is distroless: no shell, no curl, no wget, no busybox. The old
// HEALTHCHECK shelled out to `curl -fsS`, which cannot work here — there is nothing to run
// it. Rather than drag a whole distro back in just to make an HTTP request, the binary grows
// a second mode and probes itself:
//
//	HEALTHCHECK CMD ["/mensa-bot", "-healthcheck"]
//
// This is the conventional answer for distroless images, and it has a real advantage over
// curl: the probe is compiled from the same source as the server, so it can never drift from
// the endpoint it checks.
func runHealthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// HTTP_ADDR is a bind address (":8080", "0.0.0.0:8080"), which is not directly dialable
	// as a URL host: an empty or wildcard host must become loopback to be probed from inside
	// the container.
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: cannot parse HTTP_ADDR %q: %v\n", addr, err)
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	url := fmt.Sprintf("http://%s/health", net.JoinHostPort(host, port))

	client := &http.Client{Timeout: healthcheckTimeout}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: %s returned %s\n", url, resp.Status)
		return 1
	}
	return 0
}
