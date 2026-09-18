package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExportRequiresClient(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"export"}, &stdout, &stderr, defaultCommandDependencies())
	if code != 2 || !strings.Contains(stderr.String(), "usage: export --client") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestExportUnknownClientRejectedByUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"export", "--client", "nope", "extra"}, &stdout, &stderr, defaultCommandDependencies())
	if code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestHelpListsExport(t *testing.T) {
	var stdout bytes.Buffer
	printHelp(&stdout)
	if !strings.Contains(stdout.String(), "export") {
		t.Fatalf("help=%s", stdout.String())
	}
}
