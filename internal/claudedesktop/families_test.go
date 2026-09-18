package claudedesktop

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// unsupportedNativeKeys are the field names #255 originally expected Claude
// Desktop to accept: family routes, a model identity, an inference base URL, a
// proxy endpoint, and credentials. The native contract accepts none of them, so
// every test here is a disposition proof rather than a feature test.
var unsupportedNativeKeys = []string{
	"opus", "sonnet", "haiku", "fable",
	"model", "modelMap", "tierModels", "assignments", "defaults",
	"baseUrl", "baseURL", "base_url", "anthropicBaseUrl", "inferenceBaseUrl",
	"proxy", "proxyUrl", "proxy_url", "endpoint",
	"apiKey", "api_key", "token", "authToken", "env",
}

func TestUnsupportedRoutingKeysMakeAManagedEntryUnreadable(t *testing.T) {
	for _, key := range unsupportedNativeKeys {
		t.Run(key, func(t *testing.T) {
			path := configPathForHome(t.TempDir())
			body := `{"mcpServers":{"benes":{"command":"benes","args":["mcp"],"` + key + `":"value"}}}`
			writeConfigFile(t, path, body)
			if got := mustObservePath(t, path).Kind; got != ObservedEntryUnusable {
				t.Fatalf("kind=%q for key %s", got, key)
			}
		})
	}
}

func TestInstallRefusesDocumentsCarryingUnsupportedRoutingKeys(t *testing.T) {
	for _, key := range unsupportedNativeKeys {
		t.Run(key, func(t *testing.T) {
			path := configPathForHome(t.TempDir())
			body := `{"mcpServers":{"benes":{"command":"benes","args":["mcp"],"` + key + `":"value"}}}`
			writeConfigFile(t, path, body)
			before := readConfigFile(t, path)
			if _, err := InstallNative(path, managedFixture); !errors.Is(err, ErrForeignEntry) {
				t.Fatalf("key %s err=%v", key, err)
			}
			if after := readConfigFile(t, path); after != before {
				t.Fatalf("key %s was rewritten:\n%s", key, after)
			}
		})
	}
}

// TestManagedProjectionSerializesNoRoutingVocabulary is the serialization half
// of the disposition: whatever the desired projection contains, the managed
// entry Benes writes carries exactly a command and an argument list.
func TestManagedProjectionSerializesNoRoutingVocabulary(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, userConfigFixture)
	// A projection whose arguments happen to name every family is still only a
	// command and an argument list; a family name is not a routing key.
	projection := Projection{
		Command: "benes",
		Args:    append([]string{"mcp", "serve"}, unsupportedNativeKeys...),
	}
	result, refusal := Apply(host.input(&projection, true))
	if refusal != nil {
		t.Fatalf("refusal=%+v", refusal)
	}
	if !result.Applied {
		t.Fatalf("result=%+v", result)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readConfigFile(t, host.path)), &doc); err != nil {
		t.Fatal(err)
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(doc[mcpServersKey], &servers); err != nil {
		t.Fatal(err)
	}
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(servers[ManagedEntryName], &entry); err != nil {
		t.Fatal(err)
	}
	if len(entry) != 2 {
		t.Fatalf("managed entry keys=%v", entry)
	}
	for key := range entry {
		if key != "command" && key != "args" {
			t.Fatalf("managed entry serialized %q", key)
		}
	}
	// No routing key was added anywhere else in the document either.
	for _, key := range unsupportedNativeKeys {
		if key == "env" {
			continue
		}
		if _, ok := doc[key]; ok {
			t.Fatalf("root gained routing key %q", key)
		}
	}
}

func TestStatusCarriesNoFamilyRoutingVocabulary(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, userConfigFixture)
	for _, status := range []Status{
		Evaluate(host.input(ManagedNativeProjection(), true)),
		Evaluate(host.input(&managedFixture, true)),
	} {
		body := statusJSON(t, status)
		for _, key := range unsupportedNativeKeys {
			if strings.Contains(body, `"`+key+`"`) {
				t.Fatalf("status carries routing key %q: %s", key, body)
			}
		}
	}
}

// TestNoFamilyAssignmentReachesTheNativeProjection records where historical
// Benes family data would have to enter the contract, and proves it cannot.
// Claude Desktop's supported local configuration has no inference or model
// routing surface, so the contract has no field for a family, a model, a base
// URL, or a credential, and a projection can only be a command plus arguments.
func TestNoFamilyAssignmentReachesTheNativeProjection(t *testing.T) {
	if got := DesiredProjection(Input{DesiredEnabled: true, Managed: nil}); got != nil {
		t.Fatalf("projection without a managed runtime=%+v", got)
	}
	if got := DesiredProjection(Input{DesiredEnabled: false, Managed: &managedFixture}); got != nil {
		t.Fatalf("projection while disabled=%+v", got)
	}
	canonical := managedFixture.Canonical()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 {
		t.Fatalf("projection fields=%v", decoded)
	}
	// A projection with a family name in the executable position is rejected by
	// the validator only for being empty; the contract has nowhere to put a
	// family assignment, so no serialization can carry one.
	for _, key := range unsupportedNativeKeys {
		if _, ok := decoded[key]; ok {
			t.Fatalf("projection carries %q", key)
		}
	}
}
