package main

import "github.com/Wibias/Benes/internal/runtimestate"

func evaluateRuntime(state runtimePortState, err error, deps commandDependencies) runtimestate.Verdict {
	if err != nil {
		return runtimestate.Classify(runtimestate.Record{}, runtimestate.ProcessInfo{}, false)
	}
	rec := runtimestate.Record{PID: state.PID, Port: state.Port, Hostname: state.Hostname, Exe: state.Exe}
	inspect := runtimestate.Inspect
	if deps.inspectProcess != nil {
		inspect = deps.inspectProcess
	}
	probe := runtimestate.ProbeReadyz
	if deps.probeListener != nil {
		probe = deps.probeListener
	}
	ready := false
	if host, ok := runtimestate.LoopbackProbeHost(state.Hostname); ok {
		ready = probe(host, state.Port)
	}
	return runtimestate.Classify(rec, inspect(state.PID), ready)
}
