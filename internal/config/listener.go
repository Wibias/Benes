package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	DefaultListenerHostname = "127.0.0.1"
	DefaultListenerPort     = 23100
	DefaultDevPort          = 23200
)

type ListenerConfig struct {
	Hostname string
	Port     int
}

func ProjectListener(disk DiskConfig) (ListenerConfig, error) {
	result := ListenerConfig{Hostname: DefaultListenerHostname, Port: DefaultListenerPort}
	if len(bytes.TrimSpace(disk.Raw)) == 0 {
		return ListenerConfig{}, fmt.Errorf("listener projection requires raw disk config")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(disk.Raw, &root); err != nil || root == nil {
		if err == nil {
			err = fmt.Errorf("root must be an object")
		}
		return ListenerConfig{}, fmt.Errorf("project listener: %w", err)
	}
	if raw, ok := root["hostname"]; ok {
		var hostname string
		if isJSONNull(raw) || json.Unmarshal(raw, &hostname) != nil {
			return ListenerConfig{}, fmt.Errorf("project listener: hostname must be a string")
		}
		hostname = strings.TrimSpace(hostname)
		switch {
		case hostname == "":
			result.Hostname = DefaultListenerHostname
		case strings.EqualFold(hostname, "localhost"):
			result.Hostname = DefaultListenerHostname
		default:
			result.Hostname = hostname
		}
	}
	if raw, ok := root["port"]; ok {
		if isJSONNull(raw) {
			return ListenerConfig{}, fmt.Errorf("project listener: port must be an integer from 1 through 65535")
		}
		var number float64
		if err := json.Unmarshal(raw, &number); err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < 1 || number > 65535 {
			return ListenerConfig{}, fmt.Errorf("project listener: port must be an integer from 1 through 65535")
		}
		result.Port = int(number)
	}
	return result, nil
}
