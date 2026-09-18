package platform

import (
	"context"
	"os"
	"sync"
	"time"
)

type ServiceManager string

const (
	ServiceManagerUnknown     ServiceManager = "unknown"
	ServiceManagerUnavailable ServiceManager = "unavailable"
	ServiceManagerSystemd     ServiceManager = "systemd"
	ServiceManagerWindowsSCM  ServiceManager = "windows-scm"
	ServiceManagerLaunchd     ServiceManager = "launchd"
	ServiceManagerScheduler   ServiceManager = "scheduler"
)

type Snapshot struct {
	At             time.Time
	ServiceManager ServiceManager
	Complete       bool
}

type Probe struct {
	now func() time.Time

	mu       sync.Mutex
	inflight bool
	snap     Snapshot
}

func NewProbe() *Probe {
	return &Probe{now: time.Now, snap: Snapshot{ServiceManager: ServiceManagerUnknown}}
}

func (p *Probe) Snapshot() Snapshot {
	if p == nil {
		return Snapshot{ServiceManager: ServiceManagerUnknown}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snap
}

func (p *Probe) Refresh(ctx context.Context) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	if p.inflight {
		p.mu.Unlock()
		return false
	}
	p.inflight = true
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			p.inflight = false
			p.mu.Unlock()
		}()
		select {
		case <-ctx.Done():
			return
		default:
		}
		snap := Snapshot{At: p.now(), ServiceManager: detectServiceManager(), Complete: true}
		p.mu.Lock()
		p.snap = snap
		p.mu.Unlock()
	}()
	return true
}

func detectServiceManager() ServiceManager {
	return classifyServiceManager(pathExists)
}

func classifyServiceManager(exists func(string) bool) ServiceManager {
	if exists("/bin/launchctl") && exists("/System/Library/LaunchDaemons") {
		return ServiceManagerLaunchd
	}
	if exists("/run/systemd/system") && exists("/run/dbus/system_bus_socket") {
		return ServiceManagerSystemd
	}
	if exists(`C:\Windows\System32\services.exe`) {
		return ServiceManagerWindowsSCM
	}
	if exists(`C:\Windows\System32\schtasks.exe`) {
		return ServiceManagerScheduler
	}
	if exists("/bin/systemctl") || exists("/usr/bin/systemctl") {
		return ServiceManagerUnavailable
	}
	return ServiceManagerUnknown
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
