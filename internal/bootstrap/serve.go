package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	runtimecore "github.com/Wibias/Benes/internal/runtime"
	"github.com/Wibias/Benes/internal/store/atomicfile"
)

type ServeOptions struct {
	Bind            BindOptions
	HTTP            runtimecore.HTTPServerOptions
	PIDPath         string
	RuntimePortPath string
	AfterListen     func(hostname string, port int) error
}

func ServeDataPlane(ctx context.Context, plane DataPlane, options ServeOptions) error {
	if ctx == nil {
		return fmt.Errorf("data-plane context is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if setter, ok := plane.Handler.(interface{ SetStop(func()) }); ok {
		setter.SetStop(cancel)
	}
	listener, err := BindDataPlane(plane, options.Bind)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer plane.Close()
	if err := publishRuntimeState(listener, plane, options); err != nil {
		return err
	}
	defer clearRuntimeState(options)
	if err := invokeAfterListen(listener, plane, options); err != nil {
		return err
	}
	reconcileAfterListen(listener, plane)
	if plane.startStorage != nil {
		plane.startStorage(ctx)
	}

	if err := runtimecore.ServeHTTP(ctx, listener, plane.Handler, options.HTTP); err != nil {
		return fmt.Errorf("serve data plane: %w", err)
	}
	return nil
}

// reconcileAfterListen lets the data plane reconcile the projections it owns against the address
// it actually bound. It runs after the caller's AfterListen hook, and it deliberately cannot abort
// the serve: a projection that could not be written is reported by the plane through its own
// state, and the listener keeps serving.
func reconcileAfterListen(listener net.Listener, plane DataPlane) {
	reconciler, ok := plane.Handler.(interface{ ReconcileAfterListen(string, int) })
	if !ok {
		return
	}
	hostname, port := listenAddress(listener, plane)
	reconciler.ReconcileAfterListen(hostname, port)
}

// listenAddress is the address the data plane is reachable on: the real listener port when the
// bind asked for an ephemeral one, otherwise the configured one.
func listenAddress(listener net.Listener, plane DataPlane) (string, int) {
	port := plane.Listener.Port
	if addr, ok := listener.Addr().(*net.TCPAddr); ok && addr.Port > 0 {
		port = addr.Port
	}
	hostname := strings.TrimSpace(plane.Listener.Hostname)
	if hostname == "" {
		hostname = "127.0.0.1"
	}
	return hostname, port
}

func publishRuntimeState(listener net.Listener, plane DataPlane, options ServeOptions) error {
	if strings.TrimSpace(options.PIDPath) == "" && strings.TrimSpace(options.RuntimePortPath) == "" {
		return nil
	}
	pid := os.Getpid()
	port := plane.Listener.Port
	if addr, ok := listener.Addr().(*net.TCPAddr); ok && addr.Port > 0 {
		port = addr.Port
	}
	if path := strings.TrimSpace(options.PIDPath); path != "" {
		body := []byte(strconv.Itoa(pid))
		if err := atomicfile.Write(path, body, atomicfile.Options{Mode: 0o600}); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}
		benesPath := filepath.Join(filepath.Dir(path), "benes.pid")
		if err := atomicfile.Write(benesPath, body, atomicfile.Options{Mode: 0o600}); err != nil {
			return fmt.Errorf("write benes pid file: %w", err)
		}
	}
	if path := strings.TrimSpace(options.RuntimePortPath); path != "" {
		record := map[string]any{
			"pid":      pid,
			"port":     port,
			"hostname": plane.Listener.Hostname,
		}
		if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
			record["exe"] = exe
		}
		body, err := json.Marshal(record)
		if err != nil {
			return err
		}
		body = append(body, '\n')
		if err := atomicfile.Write(path, body, atomicfile.Options{Mode: 0o600}); err != nil {
			return fmt.Errorf("write runtime-port file: %w", err)
		}
	}
	return nil
}

func invokeAfterListen(listener net.Listener, plane DataPlane, options ServeOptions) error {
	if options.AfterListen == nil {
		return nil
	}
	hostname, port := listenAddress(listener, plane)
	if err := options.AfterListen(hostname, port); err != nil {
		return fmt.Errorf("after listen: %w", err)
	}
	return nil
}

func clearRuntimeState(options ServeOptions) {
	if path := strings.TrimSpace(options.PIDPath); path != "" {
		_ = os.Remove(path)
		_ = os.Remove(filepath.Join(filepath.Dir(path), "benes.pid"))
	}
	if path := strings.TrimSpace(options.RuntimePortPath); path != "" {
		_ = os.Remove(path)
	}
}
