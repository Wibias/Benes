package bootstrap

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type ListenFunc func(network, address string) (net.Listener, error)

type BindOptions struct {
	Listen ListenFunc
}

func BindDataPlane(plane DataPlane, options BindOptions) (net.Listener, error) {
	if !plane.bindReady {
		return nil, fmt.Errorf("data plane is not authorized for binding")
	}
	if plane.Handler == nil {
		return nil, fmt.Errorf("data-plane handler is required before binding")
	}
	host := strings.TrimSpace(plane.Listener.Hostname)
	if host == "" {
		return nil, fmt.Errorf("data-plane bind hostname is required")
	}
	if plane.Listener.Port < 1 || plane.Listener.Port > 65535 {
		return nil, fmt.Errorf("data-plane bind port must be from 1 through 65535")
	}
	listen := options.Listen
	if listen == nil {
		listen = net.Listen
	}
	address := net.JoinHostPort(host, strconv.Itoa(plane.Listener.Port))
	listener, err := listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("bind data plane on %s: %w", address, err)
	}
	if listener == nil {
		return nil, fmt.Errorf("bind data plane on %s: listener factory returned nil", address)
	}
	return listener, nil
}
