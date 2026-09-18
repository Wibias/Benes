package harnesspolicy

import "testing"

func activationPtr(value Activation) *Activation {
	return &value
}

func TestActivationValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   Activation
		wantErr bool
	}{
		{name: "enabled", value: ActivationEnabled},
		{name: "disabled", value: ActivationDisabled},
		{name: "empty", value: Activation(""), wantErr: true},
		{name: "inherit is represented by absence", value: Activation("inherit"), wantErr: true},
		{name: "unknown", value: Activation("sometimes"), wantErr: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.value.Validate()
			if test.wantErr && err == nil {
				t.Fatalf("Validate() error = nil, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestOverridesValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		overrides Overrides
		wantErr   bool
	}{
		{name: "empty inherits both"},
		{
			name: "independent valid overrides",
			overrides: Overrides{
				WebSearch: activationPtr(ActivationEnabled),
				Vision:    activationPtr(ActivationDisabled),
			},
		},
		{
			name: "invalid web search",
			overrides: Overrides{
				WebSearch: activationPtr(Activation("inherit")),
				Vision:    activationPtr(ActivationDisabled),
			},
			wantErr: true,
		},
		{
			name: "invalid vision",
			overrides: Overrides{
				WebSearch: activationPtr(ActivationEnabled),
				Vision:    activationPtr(Activation("auto")),
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.overrides.Validate()
			if test.wantErr && err == nil {
				t.Fatalf("Validate() error = nil, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   ResolveInput
		want    Decision
		wantErr bool
	}{
		{
			name: "proven native request wins over disabled harness and global policy",
			input: ResolveInput{
				GlobalEnabled:   false,
				Override:        activationPtr(ActivationDisabled),
				NativeRequested: true,
				NativeProven:    true,
				SidecarProven:   false,
			},
			want: Decision{Enabled: true, Native: true, Source: SourceRequestNative},
		},
		{
			name: "harness enabled overrides global disabled",
			input: ResolveInput{
				GlobalEnabled: false,
				Override:      activationPtr(ActivationEnabled),
				SidecarProven: true,
			},
			want: Decision{Enabled: true, Source: SourceHarnessOverride},
		},
		{
			name: "harness disabled overrides global enabled",
			input: ResolveInput{
				GlobalEnabled: true,
				Override:      activationPtr(ActivationDisabled),
				SidecarProven: true,
			},
			want: Decision{Enabled: false, Source: SourceHarnessOverride},
		},
		{
			name: "absent override inherits global enabled",
			input: ResolveInput{
				GlobalEnabled: true,
				SidecarProven: true,
			},
			want: Decision{Enabled: true, Source: SourceGlobal},
		},
		{
			name: "absent override inherits global disabled",
			input: ResolveInput{
				GlobalEnabled: false,
				SidecarProven: true,
			},
			want: Decision{Enabled: false, Source: SourceGlobal},
		},
		{
			name: "enabled policy without proven sidecar fails closed",
			input: ResolveInput{
				GlobalEnabled: true,
				SidecarProven: false,
			},
			want: Decision{Enabled: false, Source: SourceUnsupported},
		},
		{
			name: "unproven native request falls back to enabled sidecar",
			input: ResolveInput{
				GlobalEnabled:   false,
				Override:        activationPtr(ActivationEnabled),
				NativeRequested: true,
				NativeProven:    false,
				SidecarProven:   true,
			},
			want: Decision{Enabled: true, Source: SourceHarnessOverride},
		},
		{
			name: "unproven native request respects disabled sidecar policy",
			input: ResolveInput{
				GlobalEnabled:   true,
				Override:        activationPtr(ActivationDisabled),
				NativeRequested: true,
				NativeProven:    false,
				SidecarProven:   true,
			},
			want: Decision{Enabled: false, Source: SourceHarnessOverride},
		},
		{
			name: "invalid override is rejected",
			input: ResolveInput{
				GlobalEnabled: true,
				Override:      activationPtr(Activation("inherit")),
				SidecarProven: true,
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Resolve(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("Resolve() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v, want nil", err)
			}
			if got != test.want {
				t.Fatalf("Resolve() = %#v, want %#v", got, test.want)
			}
		})
	}
}
