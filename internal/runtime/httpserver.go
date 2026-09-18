package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	defaultReadHeaderTimeout = 10 * time.Second
	defaultReadTimeout       = 30 * time.Second
	defaultIdleTimeout       = 2 * time.Minute
	defaultShutdownTimeout   = 10 * time.Second
	defaultMaxHeaderBytes    = 1 << 20
)

type HTTPServerOptions struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

func newHTTPServer(ctx context.Context, handler http.Handler, options HTTPServerOptions) (*http.Server, time.Duration) {
	readHeaderTimeout := options.ReadHeaderTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = defaultReadHeaderTimeout
	}
	readTimeout := options.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = defaultReadTimeout
	}
	idleTimeout := options.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleTimeout
	}
	shutdownTimeout := options.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}
	maxHeaderBytes := options.MaxHeaderBytes
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = defaultMaxHeaderBytes
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      0,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	return server, shutdownTimeout
}

func ServeHTTP(ctx context.Context, listener net.Listener, handler http.Handler, options HTTPServerOptions) error {
	if ctx == nil {
		return fmt.Errorf("HTTP context is required")
	}
	if listener == nil {
		return fmt.Errorf("HTTP listener is required")
	}
	if handler == nil {
		return fmt.Errorf("HTTP handler is required")
	}

	server, shutdownTimeout := newHTTPServer(ctx, handler, options)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)

	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		shutdownErr := server.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			closeErr := server.Close()
			serveResult := <-serveErr
			if closeErr != nil {
				return fmt.Errorf("shutdown HTTP server: %w; force close: %v", shutdownErr, closeErr)
			}
			if serveResult != nil && !errors.Is(serveResult, http.ErrServerClosed) {
				return fmt.Errorf("shutdown HTTP server: %w; serve: %v", shutdownErr, serveResult)
			}
			return fmt.Errorf("shutdown HTTP server: %w", shutdownErr)
		}

		serveResult := <-serveErr
		if serveResult != nil && !errors.Is(serveResult, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", serveResult)
		}
		return nil
	}
}
