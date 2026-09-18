package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "benes ") {
		t.Fatalf("stdout = %q, want benes version line", stdout.String())
	}
}

func TestRunUnknownCommandFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"does-not-exist"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q, want unknown command message", stderr.String())
	}
}

func TestRunUpdatePrintsPackageManagerPolicyAndDoesNotMutate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"update"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "does not self-update") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "package manager") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunHelpMentionsDev(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "  dev        ") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunLabWithoutArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"lab"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stderr.String(), "benes: usage: lab help|rebuild|status") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunHelpListsLab(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout.String(), "  lab        ") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunStartIsServeAlias(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"start", "--help-not-a-flag"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "does not accept arguments") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
