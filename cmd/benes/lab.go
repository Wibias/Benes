package main

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/lab"
)

func runLab(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: usage: lab help|rebuild|status")
		return 2
	}
	switch args[0] {
	case "help":
		fmt.Fprintln(stdout, "benes lab help|rebuild|status")
		fmt.Fprintln(stdout, "  rebuild  Record protocol-conformance verdicts from config.json (no provider HTTP)")
		fmt.Fprintln(stdout, "  status   Print lab store counts")
		return 0
	case "rebuild":
		return runLabRebuild(stdout, stderr, deps)
	case "status":
		return runLabStatus(stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: lab help|rebuild|status")
		return 2
	}
}

func runLabRebuild(stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	disk, err := deps.loadDiskConfig(paths.Config, 0)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	dir := filepath.Join(filepath.Dir(paths.Config), "lab")
	result, err := lab.Rebuild(dir, disk, time.Now().UnixMilli())
	if err != nil {
		return serveFailure(stderr, "rebuild lab", err)
	}
	fmt.Fprintf(stdout, "rebuilt subjects=%d verdicts=%d events=%d\n", result.SubjectCount, result.VerdictCount, result.EventCount)
	return 0
}

func runLabStatus(stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	dir := filepath.Join(filepath.Dir(paths.Config), "lab")
	projection, err := lab.LoadProjection(dir)
	if err != nil {
		return serveFailure(stderr, "load lab projection", err)
	}
	subjects, verdicts, events := 0, 0, 0
	if projection.CorruptionCount > 0 {
		fmt.Fprintf(stderr, "benes: serve: lab ledger corrupt: %v\n", lab.ErrCorrupt)
	} else {
		subjects = labSubjectCount(projection)
		verdicts = len(projection.Verdicts)
		events = len(projection.Events)
	}
	fmt.Fprintf(stdout, "subjects\t%d\n", subjects)
	fmt.Fprintf(stdout, "verdicts\t%d\n", verdicts)
	fmt.Fprintf(stdout, "events\t%d\n", events)
	return 0
}

func labSubjectCount(projection lab.Projection) int {
	seen := make(map[string]struct{}, len(projection.Verdicts))
	for _, ev := range projection.Verdicts {
		if ev.SubjectID == "" {
			continue
		}
		seen[ev.SubjectID] = struct{}{}
	}
	return len(seen)
}
