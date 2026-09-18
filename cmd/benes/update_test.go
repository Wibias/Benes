package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunUpdatePrintsPolicyWithoutMutating(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runUpdate(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "never mutated by benes update") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if code := runUpdate([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("extra args code=%d stderr=%s", code, stderr.String())
	}
}
