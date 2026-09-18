package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestSoakProcessIsolatedWavesReturnToIdle(t *testing.T) {
	if os.Getenv("BENES_SOAK_CHILD") == "1" {
		runSoakChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSoakProcessIsolatedWavesReturnToIdle$", "-test.count=1", "-test.v=false")
	cmd.Env = append(os.Environ(), "BENES_SOAK_CHILD=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	addr, err := readSoakReady(stdout, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr
	body := `{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`
	client := &http.Client{Timeout: 5 * time.Second}
	for range 10 {
		var wg sync.WaitGroup
		for range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req, err := http.NewRequest(http.MethodPost, base+"/v1/responses", strings.NewReader(body))
				if err != nil {
					t.Error(err)
					return
				}
				req.Header.Set("Authorization", "Bearer local-secret")
				resp, err := client.Do(req)
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("status=%d", resp.StatusCode)
				}
			}()
		}
		wg.Wait()
	}
	metrics, err := fetchSoakMetrics(client, base)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["activeTurns"] != float64(0) || metrics["processBytes"] != float64(0) || metrics["activeReaders"] != float64(0) || metrics["budgetWaiters"] != float64(0) {
		t.Fatalf("child not idle: %#v", metrics)
	}
}

func readSoakReady(r io.Reader, timeout time.Duration) (string, error) {
	type result struct {
		addr string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "READY ") {
				ch <- result{addr: strings.TrimSpace(strings.TrimPrefix(line, "READY "))}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{err: fmt.Errorf("child closed before READY")}
	}()
	select {
	case got := <-ch:
		return got.addr, got.err
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out waiting for soak child")
	}
}

func fetchSoakMetrics(client *http.Client, base string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, base+"/api/resource-metrics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("metrics status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return nil, err
	}
	return got, nil
}

func runSoakChild() {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("READY %s\n", ln.Addr().String())
	_ = http.Serve(ln, h)
}
