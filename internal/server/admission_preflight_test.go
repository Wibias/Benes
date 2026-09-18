package server

import "testing"

func TestValidateDataPlaneAdmissionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  DataPlaneAdmissionPolicy
		wantErr bool
	}{
		{"loopback no credential", DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"}, false},
		{"remote credential", DataPlaneAdmissionPolicy{BindHostname: "0.0.0.0", DataPlaneTokens: []string{"k"}}, false},
		{"remote no credential", DataPlaneAdmissionPolicy{BindHostname: "0.0.0.0"}, true},
		{"blank bind", DataPlaneAdmissionPolicy{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDataPlaneAdmissionPolicy(tc.policy)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
