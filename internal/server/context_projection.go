package server

import (
	"encoding/json"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/contextprojection"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
)

type providerProjection struct {
	registry  *contextprojection.Registry
	canonical protocol.Context
	active    bool
}

func (h *handler) contextProjectionMode() contextprojection.Mode {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return contextprojection.ModeOff
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return contextprojection.ModeOff
	}
	var root struct {
		ContextProjection struct {
			Mode string `json:"mode"`
		} `json:"contextProjection"`
	}
	if json.Unmarshal(disk.Raw, &root) != nil {
		return contextprojection.ModeOff
	}
	return contextprojection.Mode(strings.TrimSpace(root.ContextProjection.Mode))
}

func (h *handler) projectProviderContext(request *protocol.ParsedRequest, provider providercontract.Responses, compaction bool, webSearch bool) providerProjection {
	if request == nil {
		return providerProjection{}
	}
	if contextprojection.EmergencyDisabled() {
		return providerProjection{}
	}
	mode := h.contextProjectionMode()
	if mode == "" || mode == contextprojection.ModeOff {
		return providerProjection{}
	}
	epoch, ok := contextprojection.ResolveEpoch(contextprojection.EpochInput{
		PreviousResponseID: request.PreviousResponseID,
		RequestedMode:      mode,
	})
	if !ok || epoch.State != contextprojection.StateActive {
		return providerProjection{}
	}
	if isNativeCodexForward(provider) || compaction {
		return providerProjection{}
	}
	if request.StructuredOutput {
		return providerProjection{}
	}
	if request.Options.ToolChoice != nil && request.Options.ToolChoice.Kind != protocol.ToolChoiceAuto && request.Options.ToolChoice.Kind != "" {
		return providerProjection{}
	}
	switch mode {
	case contextprojection.ModeShadow:
		contextprojection.ObserveShadow(request.Context, contextprojection.PlanOptions{})
		return providerProjection{}
	case contextprojection.ModeDuplicate:
		plan := contextprojection.Plan(request.Context, contextprojection.DuplicatePolicy(), contextprojection.PlanOptions{})
		request.Context = contextprojection.Apply(request.Context, plan)
		return providerProjection{}
	case contextprojection.ModeRecovery, contextprojection.ModeOn:
		if webSearch {
			plan := contextprojection.Plan(request.Context, contextprojection.DuplicatePolicy(), contextprojection.PlanOptions{})
			request.Context = contextprojection.Apply(request.Context, plan)
			return providerProjection{}
		}
		canonical := contextprojection.Apply(request.Context, contextprojection.PlanResult{})
		plan := contextprojection.Plan(request.Context, contextprojection.RecoveryPolicy(), contextprojection.PlanOptions{})
		projected := contextprojection.Apply(request.Context, plan)
		if len(plan.Replacements) == 0 {
			request.Context = projected
			return providerProjection{}
		}
		tools, err := contextprojection.AddRecoveryTool(projected.Tools)
		if err != nil {
			request.Context = projected
			return providerProjection{}
		}
		projected.Tools = tools
		request.Context = projected
		return providerProjection{
			registry:  plan.Registry,
			canonical: canonical,
			active:    true,
		}
	}
	return providerProjection{}
}
