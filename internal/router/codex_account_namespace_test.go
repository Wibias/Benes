package router

import "testing"

func TestParseExplicitWithCodexAccountsRoutesExactNamespaceToCanonicalOpenAI(t *testing.T) {
	namespaces := map[string]string{"side": "acct-b", "main": "@main"}
	for _, tc := range []struct {
		selector  string
		model     string
		accountID string
	}{
		{selector: "side/gpt-5.6", model: "gpt-5.6", accountID: "acct-b"},
		{selector: "main/gpt-5.3-codex-spark", model: "gpt-5.3-codex-spark", accountID: "@main"},
	} {
		route, err := ParseExplicitWithCodexAccounts(tc.selector, namespaces)
		if err != nil {
			t.Fatal(err)
		}
		if route.Provider != "openai" || route.Model != tc.model || route.CodexAccountID != tc.accountID {
			t.Fatalf("route=%#v", route)
		}
	}
}

func TestParseExplicitWithCodexAccountsKeepsNamespaceMatchingExactCase(t *testing.T) {
	route, err := ParseExplicitWithCodexAccounts("Side/gpt-5.6", map[string]string{"side": "acct-b"})
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "Side" || route.Model != "gpt-5.6" || route.CodexAccountID != "" {
		t.Fatalf("route=%#v", route)
	}
}
