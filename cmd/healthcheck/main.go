// Command healthcheck exits 0 when the API on this host answers /health. The
// runtime image is distroless (no shell or curl), so Docker's HEALTHCHECK
// runs this instead.
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	url := "http://127.0.0.1:" + port() + "/health"
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url) // #nosec G107 -- fixed loopback URL
	if err != nil {
		os.Exit(1)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

func port() string {
	if p := os.Getenv("HTTP_PORT"); p != "" {
		return p
	}
	return "8080"
}
