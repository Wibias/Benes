//go:build !windows

package transport

import "errors"

var errSystemProxyUnsupported = errors.New("system proxy auto-discovery is not supported on this platform")

func lookupSystemProxy() (string, []string, error) {
	return "", nil, errSystemProxyUnsupported
}
