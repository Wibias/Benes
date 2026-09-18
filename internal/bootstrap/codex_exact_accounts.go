package bootstrap

import (
	"fmt"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providerregistry"
)

func validateCodexExactAccountAuthority(disk config.DiskConfig, specs []providerregistry.Spec) error {
	if len(disk.CodexAccountNamespaces) == 0 {
		return nil
	}
	for _, spec := range specs {
		if spec.ID != "openai" || spec.Protocol != providerregistry.ProtocolOpenAIResponses || spec.AuthMode != providerregistry.AuthModeForward {
			continue
		}
		if spec.CodexAccountMode == providerregistry.CodexAccountModeDirect || spec.CodexAccountMode == providerregistry.CodexAccountModePool {
			return nil
		}
	}
	return fmt.Errorf("codexAccountNamespaces require canonical openai forward auth with codexAccountMode direct or pool")
}
