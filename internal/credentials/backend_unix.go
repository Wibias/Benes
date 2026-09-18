//go:build !windows && !darwin

package credentials

func defaultBackend() Backend { return linuxBackend() }
