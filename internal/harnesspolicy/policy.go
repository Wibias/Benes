package harnesspolicy

import "fmt"

type Activation string

const (
	ActivationEnabled  Activation = "enabled"
	ActivationDisabled Activation = "disabled"
)

func (a Activation) Validate() error {
	switch a {
	case ActivationEnabled, ActivationDisabled:
		return nil
	default:
		return fmt.Errorf("invalid sidecar activation %q", a)
	}
}

type Overrides struct {
	WebSearch *Activation `json:"webSearch,omitempty"`
	Vision    *Activation `json:"vision,omitempty"`
}

func (o Overrides) Validate() error {
	if o.WebSearch != nil {
		if err := o.WebSearch.Validate(); err != nil {
			return fmt.Errorf("web search override: %w", err)
		}
	}
	if o.Vision != nil {
		if err := o.Vision.Validate(); err != nil {
			return fmt.Errorf("vision override: %w", err)
		}
	}
	return nil
}

type Source string

const (
	SourceRequestNative   Source = "request_native"
	SourceHarnessOverride Source = "harness_override"
	SourceGlobal          Source = "global"
	SourceUnsupported     Source = "unsupported"
)

type ResolveInput struct {
	GlobalEnabled   bool
	Override        *Activation
	NativeRequested bool
	NativeProven    bool
	SidecarProven   bool
}

type Decision struct {
	Enabled bool
	Native  bool
	Source  Source
}

func Resolve(input ResolveInput) (Decision, error) {
	if input.Override != nil {
		if err := input.Override.Validate(); err != nil {
			return Decision{}, err
		}
	}

	if input.NativeRequested && input.NativeProven {
		return Decision{
			Enabled: true,
			Native:  true,
			Source:  SourceRequestNative,
		}, nil
	}

	desired := input.GlobalEnabled
	source := SourceGlobal
	if input.Override != nil {
		desired = *input.Override == ActivationEnabled
		source = SourceHarnessOverride
	}

	if !desired {
		return Decision{Source: source}, nil
	}
	if !input.SidecarProven {
		return Decision{Source: SourceUnsupported}, nil
	}

	return Decision{Enabled: true, Source: source}, nil
}
