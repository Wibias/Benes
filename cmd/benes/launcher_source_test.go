package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNpmBenesLauncherExecsGoCLI(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pkg), `"./bin/benes.mjs"`) {
		t.Fatal("package.json bin does not register benes")
	}
	if !strings.Contains(string(pkg), `"start:go": "go run ./cmd/benes serve"`) {
		t.Fatal("package.json start:go does not run the Go CLI")
	}
	for _, want := range []string{`"cmd"`, `"internal"`, `"go.mod"`, `"go.sum"`} {
		if !strings.Contains(string(pkg), want) {
			t.Fatalf("package.json files missing %s", want)
		}
	}
	benes, err := os.ReadFile(filepath.Join(root, "bin", "benes.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(benes), "BENES_BIN") || !strings.Contains(string(benes), "benes.exe") {
		t.Fatal("benes launcher cannot exec a local Go CLI")
	}
	if strings.Contains(strings.ToLower(string(benes)), "src/cli/index.ts") {
		t.Fatal("launcher points at a TypeScript CLI")
	}
	if !strings.Contains(string(pkg), `"start": "node ./bin/benes.mjs start"`) {
		t.Fatal("npm start does not use the Node launcher")
	}
	if !strings.Contains(string(pkg), `"prepare:package": "node --experimental-strip-types scripts/prepare-package.ts"`) {
		t.Fatal("prepare:package must use node strip-types on prepare-package.ts")
	}
	if strings.Contains(string(pkg), `"./src/index.ts"`) {
		t.Fatal("package exports a TypeScript backend")
	}
	if strings.Contains(string(pkg), `"src",`) {
		t.Fatal("npm package ships src/")
	}
	main, err := os.ReadFile(filepath.Join(root, "bin", "package-main.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(main), `import("../src/index.ts")`) {
		t.Fatal("package main loads a TypeScript API")
	}
	restart, err := os.ReadFile(filepath.Join(root, "scripts", "benes-restart.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(restart), "go run ./cmd/benes start") {
		t.Fatal("benes-restart does not start the Go CLI")
	}
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "go-core.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "go build -o benes ./cmd/benes") {
		t.Fatal("go-core does not build the production CLI")
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "/benes\n") || !strings.Contains(string(ignore), "/benes.exe") {
		t.Fatal("built benes binary is not gitignored")
	}
}
