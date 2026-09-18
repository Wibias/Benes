//go:build darwin

package credentials

func defaultBackend() Backend { return darwinBackend() }
