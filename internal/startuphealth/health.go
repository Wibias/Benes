package startuphealth

import "github.com/Wibias/Benes/internal/codexrouting"

type Protection string

const (
	ProtectionService Protection = "service"
	ProtectionShim    Protection = "shim"
	ProtectionNone    Protection = "none"
)

type Status string

const (
	StatusNative    Status = "native"
	StatusProtected Status = "protected"
	StatusAtRisk    Status = "at-risk"
)

type Inputs struct {
	RoutingKind      codexrouting.Kind
	AutostartEnabled bool
	ServiceInstalled bool
	ServiceViable    bool
	ServiceEnabled   bool
	ServiceRunning   bool
	ServiceStale     bool
	ServiceConflict  bool
	ServiceSupported bool
	ShimInstalled    bool
	ShimHealthy      bool
	Platform         string
	DiagnosticStale  bool
}

type Report struct {
	Status                 Status            `json:"status"`
	RoutingKind            codexrouting.Kind `json:"routingKind"`
	RoutingInjected        bool              `json:"routingInjected"`
	LocalRoutingDependency bool              `json:"localRoutingDependency"`
	AutostartEnabled       bool              `json:"autostartEnabled"`
	RebootSafe             bool              `json:"rebootSafe"`
	Protection             Protection        `json:"protection"`
	ServiceInstalled       bool              `json:"serviceInstalled"`
	ServiceViable          bool              `json:"serviceViable"`
	ServiceEnabled         bool              `json:"serviceEnabled"`
	ServiceRunning         bool              `json:"serviceRunning"`
	ServiceStale           bool              `json:"serviceStale"`
	ServiceConflict        bool              `json:"serviceConflict"`
	ShimInstalled          bool              `json:"shimInstalled"`
	ShimHealthy            bool              `json:"shimHealthy"`
	ShimCoverage           string            `json:"shimCoverage"`
	ServiceSupported       bool              `json:"serviceSupported"`
	Platform               string            `json:"platform"`
	DiagnosticStale        bool              `json:"diagnosticStale"`
	RecommendedCommand     *string           `json:"recommendedCommand"`
	Commands               Commands          `json:"commands"`
}

type Commands struct {
	InstallService string `json:"installService"`
	RepairService  string `json:"repairService"`
	InstallShim    string `json:"installShim"`
	RestoreNative  string `json:"restoreNative"`
}

var commands = Commands{
	InstallService: "benes service install",
	RepairService:  "benes service repair",
	InstallShim:    "benes codex-shim install",
	RestoreNative:  "benes restore",
}

func Derive(in Inputs) Report {
	shimEffective := in.AutostartEnabled && in.ShimHealthy
	routingInjected := in.RoutingKind == codexrouting.KindBenesLocal
	localDep := in.RoutingKind == codexrouting.KindBenesLocal || in.RoutingKind == codexrouting.KindCustomLocal || in.RoutingKind == codexrouting.KindUnknown
	shimCoverage := "none"
	if shimEffective {
		shimCoverage = "cli-only"
	}
	owns := in.RoutingKind == codexrouting.KindBenesLocal
	protection := ProtectionNone
	if owns && in.ServiceViable {
		protection = ProtectionService
	} else if owns && shimEffective {
		protection = ProtectionShim
	}
	rebootSafe := !localDep || (owns && in.ServiceViable)
	status := StatusAtRisk
	if !localDep {
		status = StatusNative
	} else if rebootSafe {
		status = StatusProtected
	}
	var recommended *string
	if status == StatusAtRisk {
		cmd := commands.RestoreNative
		if in.RoutingKind != codexrouting.KindCustomLocal && in.RoutingKind != codexrouting.KindUnknown {
			if in.ServiceSupported {
				if in.ServiceInstalled && !in.ServiceConflict {
					cmd = commands.RepairService
				} else {
					cmd = commands.InstallService
				}
			}
		}
		recommended = &cmd
	}
	return Report{
		Status:                 status,
		RoutingKind:            in.RoutingKind,
		RoutingInjected:        routingInjected,
		LocalRoutingDependency: localDep,
		AutostartEnabled:       in.AutostartEnabled,
		RebootSafe:             rebootSafe,
		Protection:             protection,
		ServiceInstalled:       in.ServiceInstalled,
		ServiceViable:          in.ServiceViable,
		ServiceEnabled:         in.ServiceEnabled,
		ServiceRunning:         in.ServiceRunning,
		ServiceStale:           in.ServiceStale,
		ServiceConflict:        in.ServiceConflict,
		ShimInstalled:          in.ShimInstalled,
		ShimHealthy:            in.ShimHealthy,
		ShimCoverage:           shimCoverage,
		ServiceSupported:       in.ServiceSupported,
		Platform:               in.Platform,
		DiagnosticStale:        in.DiagnosticStale,
		RecommendedCommand:     recommended,
		Commands:               commands,
	}
}
