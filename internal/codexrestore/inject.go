package codexrestore

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Wibias/Benes/internal/harnessidentity"
	"github.com/Wibias/Benes/internal/store/atomicfile"
)

const sectionMarker = "# Auto-injected by benes"

type InjectResult struct {
	Changed bool
	Message string
}

func Inject(codexHome, baseURL string) (InjectResult, error) {
	if strings.TrimSpace(codexHome) == "" {
		return InjectResult{}, fmt.Errorf("CODEX_HOME is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		return InjectResult{}, fmt.Errorf("base URL is required")
	}
	cfg := configPath(codexHome)
	raw, err := os.ReadFile(cfg)
	if err != nil {
		if os.IsNotExist(err) {
			return InjectResult{Message: "Codex config not found; inject skipped."}, nil
		}
		return InjectResult{}, err
	}
	content := string(raw)
	baseline := userBaseline(content)
	injected := applyInject(baseline, baseURL)
	if injected == content {
		return InjectResult{Message: "benes routing already present."}, nil
	}
	if _, err := os.Stat(journalPath(codexHome)); err != nil {
		if !os.IsNotExist(err) {
			return InjectResult{}, err
		}
		if err := writeJournalFile(codexHome, baseline, injected); err != nil {
			return InjectResult{}, err
		}
	} else {
		_ = refreshJournalHash(codexHome, injected)
	}
	if err := atomicfile.Write(cfg, []byte(injected), atomicfile.Options{Mode: 0o600}); err != nil {
		return InjectResult{}, err
	}
	if hasManagedRouting(content) {
		return InjectResult{Changed: true, Message: "Updated benes routing in Codex config."}, nil
	}
	return InjectResult{Changed: true, Message: "Injected benes routing into Codex config."}, nil
}

func applyInject(content, baseURL string) string {
	rootKey := sectionMarker + "\nmodel_provider = \"benes\"\n"
	providerTable := sectionMarker + "\n" +
		"[model_providers.benes]\n" +
		"name = \"Benes\"\n" +
		fmt.Sprintf("base_url = %q\n", baseURL) +
		"wire_api = \"responses\"\n" +
		fmt.Sprintf(`http_headers = { "X-Benes-Surface" = "codex", %q = "codex" }`, harnessidentity.Header) + "\n"

	body := strings.TrimRight(content, "\n")
	idx := indexOfFirstTableHeader(body)
	if idx < 0 {
		if body != "" {
			body += "\n\n"
		}
		return body + rootKey + "\n" + providerTable
	}
	prefix := strings.TrimRight(body[:idx], "\n")
	suffix := strings.Trim(body[idx:], "\n")
	if prefix != "" {
		prefix += "\n\n"
	}
	return prefix + rootKey + "\n" + suffix + "\n\n" + providerTable
}

func indexOfFirstTableHeader(content string) int {
	offset := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			return offset
		}
		offset += len(line) + 1
	}
	return -1
}

func writeJournalFile(codexHome, original, injected string) error {
	if _, err := os.Stat(journalPath(codexHome)); err == nil {
		return nil
	}
	j := journal{
		Version:            1,
		OriginalConfig:     base64.StdEncoding.EncodeToString([]byte(original)),
		OriginalProfile:    nil,
		InjectedConfigHash: sha256Hex(injected),
		PID:                os.Getpid(),
	}
	body, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return atomicfile.Write(journalPath(codexHome), append(body, '\n'), atomicfile.Options{Mode: 0o600})
}

func refreshJournalHash(codexHome, injected string) error {
	path := journalPath(codexHome)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var j journal
	if json.Unmarshal(raw, &j) != nil || j.Version != 1 {
		return nil
	}
	j.InjectedConfigHash = sha256Hex(injected)
	body, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(body, '\n'), atomicfile.Options{Mode: 0o600})
}
