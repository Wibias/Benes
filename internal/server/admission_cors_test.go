package server

import "testing"

func TestExplicitAllowedHTTPOriginUsesOriginCanonicalization(t *testing.T) {
	policy := DataPlaneAdmissionPolicy{
		BindHostname:     "127.0.0.1",
		CORSAllowOrigins: []string{"https://Example.COM:443/dashboard/path?ignored=1"},
	}
	admission, err := buildDataPlaneAdmission(Options{AdmissionPolicy: &policy})
	if err != nil {
		t.Fatal(err)
	}
	if status, ok := admission.admit(requestView{host: "localhost:23100", origin: "https://example.com"}); !ok || status != 0 {
		t.Fatalf("status=%d allowed=%v", status, ok)
	}
}

func TestExplicitAllowedExtensionOriginIsAuthorityScoped(t *testing.T) {
	policy := DataPlaneAdmissionPolicy{
		BindHostname:     "127.0.0.1",
		CORSAllowOrigins: []string{"chrome-extension://ABCDEF/options.html"},
	}
	admission, err := buildDataPlaneAdmission(Options{AdmissionPolicy: &policy})
	if err != nil {
		t.Fatal(err)
	}
	if status, ok := admission.admit(requestView{host: "localhost:23100", origin: "chrome-extension://abcdef/popup.html"}); !ok || status != 0 {
		t.Fatalf("configured extension rejected: status=%d allowed=%v", status, ok)
	}
	if status, ok := admission.admit(requestView{host: "localhost:23100", origin: "chrome-extension://other/popup.html"}); ok || status != 403 {
		t.Fatalf("different extension admitted: status=%d allowed=%v", status, ok)
	}
}

func TestExplicitAllowedOriginsDoNotTreatStarAsWildcard(t *testing.T) {
	policy := DataPlaneAdmissionPolicy{
		BindHostname:     "0.0.0.0",
		DataPlaneTokens:  []string{"secret"},
		CORSAllowOrigins: []string{"*"},
	}
	admission, err := buildDataPlaneAdmission(Options{AdmissionPolicy: &policy})
	if err != nil {
		t.Fatal(err)
	}
	if status, ok := admission.admit(requestView{host: "gateway.example:23100", origin: "https://evil.example", dedicatedKey: "secret"}); ok || status != 403 {
		t.Fatalf("star acted as wildcard: status=%d allowed=%v", status, ok)
	}
}

func TestExplicitAllowedOpaqueNullOriginUsesExactFallbackOnly(t *testing.T) {
	policy := DataPlaneAdmissionPolicy{
		BindHostname:     "127.0.0.1",
		CORSAllowOrigins: []string{"null"},
	}
	admission, err := buildDataPlaneAdmission(Options{AdmissionPolicy: &policy})
	if err != nil {
		t.Fatal(err)
	}
	if status, ok := admission.admit(requestView{host: "localhost:23100", origin: "null"}); !ok || status != 0 {
		t.Fatalf("exact opaque origin rejected: status=%d allowed=%v", status, ok)
	}
}

func TestAdmissionSnapshotsConfiguredOrigins(t *testing.T) {
	origins := []string{"https://one.example"}
	policy := DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1", CORSAllowOrigins: origins}
	admission, err := buildDataPlaneAdmission(Options{AdmissionPolicy: &policy})
	if err != nil {
		t.Fatal(err)
	}
	origins[0] = "https://two.example"
	policy.CORSAllowOrigins[0] = "https://two.example"
	if status, ok := admission.admit(requestView{host: "localhost:23100", origin: "https://one.example"}); !ok || status != 0 {
		t.Fatalf("snapshot origin lost: status=%d allowed=%v", status, ok)
	}
	if status, ok := admission.admit(requestView{host: "localhost:23100", origin: "https://two.example"}); ok || status != 403 {
		t.Fatalf("post-build mutation changed admission: status=%d allowed=%v", status, ok)
	}
}
