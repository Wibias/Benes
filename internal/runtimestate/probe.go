package runtimestate

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func ProbeReadyz(host string, port int) bool {
	loopback, ok := LoopbackProbeHost(host)
	if !ok || port <= 0 {
		return false
	}
	client := &http.Client{Timeout: 750 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/readyz", loopback, port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode == http.StatusOK
}
