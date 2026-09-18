package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

func fabricDelegateTool() protocol.Tool {
	return protocol.Tool{
		Name:        fabric.FabricDelegateToolName,
		Description: "Hand off exactly one fenced child turn. Provide a self-contained instruction; do not include secrets.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"instruction": map[string]any{
					"type":        "string",
					"description": "Self-contained child instruction (max 16KiB).",
				},
			},
			"required":             []string{"instruction"},
			"additionalProperties": false,
		},
	}
}

func parseDelegateInstruction(arguments string) (string, error) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		arguments = "{}"
	}
	var body struct {
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal([]byte(arguments), &body); err != nil {
		return "", fabric.InvalidTask("delegate arguments must be JSON")
	}
	if err := fabric.ValidateDelegateInstruction(body.Instruction); err != nil {
		return "", err
	}
	return body.Instruction, nil
}

// fabricPrimaryToolDecision classifies primary tool-call results when delegation.model is set.
type fabricPrimaryToolDecision int

const (
	fabricPrimaryNoDelegate fabricPrimaryToolDecision = iota
	fabricPrimaryOneDelegate
	fabricPrimaryReject
)

// decideFabricPrimaryToolCalls fail-closes ambiguous/multi tool-call primary results.
// Semantics when delegation.model is configured:
//   - zero tool calls → normal non-delegated completion
//   - exactly one reserved Fabric delegate → one child
//   - any other shape (multiple calls, non-delegate, multiple delegates) → reject
//     before BeginChildHandoff (no silent first-pick).
func decideFabricPrimaryToolCalls(calls []modelTurnToolCall) (modelTurnToolCall, fabricPrimaryToolDecision, string) {
	if len(calls) == 0 {
		return modelTurnToolCall{}, fabricPrimaryNoDelegate, ""
	}
	if len(calls) != 1 {
		return modelTurnToolCall{}, fabricPrimaryReject, "fabric_primary_ambiguous_tool_calls"
	}
	if calls[0].Name != fabric.FabricDelegateToolName {
		return modelTurnToolCall{}, fabricPrimaryReject, "fabric_primary_unexpected_tool_call"
	}
	return calls[0], fabricPrimaryOneDelegate, ""
}

// findFabricDelegateCall remains for narrow call sites/tests that only need membership.
func findFabricDelegateCall(calls []modelTurnToolCall) (modelTurnToolCall, bool) {
	call, decision, _ := decideFabricPrimaryToolCalls(calls)
	return call, decision == fabricPrimaryOneDelegate
}

type fabricContinuationMode int

const (
	fabricContUnsupported fabricContinuationMode = iota
	fabricContGoogleReplay
	fabricContOpenAINativePrevious
)

type fabricContinuationPlan struct {
	Mode              fabricContinuationMode
	NativePreviousID  string
	RequireThoughtSig bool
}

func fabricToolResultMessage(call modelTurnToolCall, childOutput string, childErr bool) protocol.Message {
	return protocol.Message{
		Role:       protocol.RoleToolResult,
		ToolCallID: call.CallID,
		ToolName:   call.Name,
		IsError:    childErr,
		Content:    []protocol.ContentPart{{Type: protocol.ContentText, Text: childOutput}},
	}
}

func fabricAssistantDelegatePart(call modelTurnToolCall) protocol.ContentPart {
	args := map[string]any{}
	_ = json.Unmarshal([]byte(strings.TrimSpace(call.Arguments)), &args)
	if args == nil {
		args = map[string]any{}
	}
	return protocol.ContentPart{
		Type:             protocol.ContentToolCall,
		ToolCallID:       call.CallID,
		ToolName:         call.Name,
		Arguments:        args,
		ThoughtSignature: call.ThoughtSignature,
		ProviderMetadata: call.ProviderMetadata,
	}
}

// buildFabricContinuationParsed builds the resume ParsedRequest under one coherent model:
//   - OpenAI Responses native-state: owned provider-native previous_response_id + ONLY the
//     new tool-result input (no replay of user/function_call alongside previous_response_id).
//   - Google thought-signature replay: full user + historical function_call (with trusted
//     ThoughtSignature) + tool result; no previous_response_id.
// ToolChoice is forced to none so continuation cannot spawn a second Fabric child.
func buildFabricContinuationParsed(primaryModel, userInput string, call modelTurnToolCall, childOutput string, childErr bool, plan fabricContinuationPlan) protocol.ParsedRequest {
	userInput = strings.TrimSpace(userInput)
	if userInput == "" {
		userInput = "proceed"
	}
	none := &protocol.ToolChoice{Kind: protocol.ToolChoiceNone}
	switch plan.Mode {
	case fabricContOpenAINativePrevious:
		return protocol.ParsedRequest{
			Source:             protocol.RequestSourceResponses,
			ModelID:            primaryModel,
			Stream:             false,
			PreviousResponseID: strings.TrimSpace(plan.NativePreviousID),
			Context: protocol.Context{
				Messages: []protocol.Message{fabricToolResultMessage(call, childOutput, childErr)},
			},
			Options: protocol.RequestOptions{ToolChoice: none, ParallelToolCalls: boolPtr(false)},
		}
	default: // google replay (and any safe replay fallback that still carries ThoughtSignature)
		msgs := []protocol.Message{
			{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: userInput}},
			},
			{
				Role:    protocol.RoleAssistant,
				Content: []protocol.ContentPart{fabricAssistantDelegatePart(call)},
			},
			fabricToolResultMessage(call, childOutput, childErr),
		}
		return protocol.ParsedRequest{
			Source:  protocol.RequestSourceResponses,
			ModelID: primaryModel,
			Stream:  false,
			Context: protocol.Context{Messages: msgs},
			Options: protocol.RequestOptions{ToolChoice: none, ParallelToolCalls: boolPtr(false)},
		}
	}
}

func terminalHasReasoningOutput(terminal map[string]any) bool {
	if terminal == nil {
		return false
	}
	raw, ok := terminal["output"].([]any)
	if !ok {
		return false
	}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := m["type"].(string)
		if typ == "reasoning" {
			return true
		}
	}
	return false
}

func fabricThoughtSignature(call modelTurnToolCall) string {
	sig := strings.TrimSpace(call.ThoughtSignature)
	if sig == "" && call.ProviderMetadata != nil && call.ProviderMetadata.Google != nil {
		sig = strings.TrimSpace(call.ProviderMetadata.Google.ThoughtSignature)
	}
	if strings.HasPrefix(sig, "fc_") {
		return ""
	}
	return sig
}

func isGoogleThoughtSignatureProtocol(protocolName string) bool {
	switch strings.TrimSpace(protocolName) {
	case "google", "google-vertex", "google-antigravity":
		return true
	default:
		return false
	}
}

func isOpenAIResponsesProtocol(protocolName string) bool {
	return strings.TrimSpace(protocolName) == "openai-responses"
}

// planFabricContinuation selects the canonical continuation model from the provider that
// actually served the primary turn (resolved protocol), not from route-name substrings.
func planFabricContinuation(servingProtocol string, call modelTurnToolCall, terminal map[string]any, nativeResponseID string) (fabricContinuationPlan, error) {
	servingProtocol = strings.TrimSpace(servingProtocol)
	nativeResponseID = strings.TrimSpace(nativeResponseID)
	switch {
	case isGoogleThoughtSignatureProtocol(servingProtocol):
		if fabricThoughtSignature(call) == "" {
			return fabricContinuationPlan{}, fabric.InvalidTransition("fabric continuation requires provider ThoughtSignature before child handoff")
		}
		return fabricContinuationPlan{Mode: fabricContGoogleReplay, RequireThoughtSig: true}, nil
	case isOpenAIResponsesProtocol(servingProtocol):
		// Preferred native-state: provider-native previous response identity only.
		// Bridge resp_* must never be used as previous_response_id.
		if nativeResponseID == "" {
			if terminalHasReasoningOutput(terminal) {
				return fabricContinuationPlan{}, fabric.InvalidTransition("fabric continuation requires provider-native previous_response_id for reasoning output")
			}
			// Non-reasoning OpenAI Responses still needs owned native previous for safe
			// tool-result continuation without undeclared historical tool replay.
			return fabricContinuationPlan{}, fabric.InvalidTransition("fabric continuation requires provider-native previous_response_id")
		}
		// NativeResponseID is captured from provider EventDone ProviderState, never from
		// TerminalResponse["id"] (Benes bridge). Bridge resp_* must not be copied there.
		return fabricContinuationPlan{Mode: fabricContOpenAINativePrevious, NativePreviousID: nativeResponseID}, nil
	default:
		// Unknown / unsupported adapter: fail closed before BeginChildHandoff rather than
		// guessing previous_response_id or inventing Fabric-specific continuation state.
		if servingProtocol == "" {
			return fabricContinuationPlan{}, fabric.InvalidTransition("fabric continuation requires resolved provider protocol")
		}
		return fabricContinuationPlan{}, fabric.InvalidTransition("fabric continuation unsupported for provider protocol " + servingProtocol)
	}
}

// validateFabricContinuationCapability fail-closes BEFORE durable child handoff when
// existing continuation mechanisms cannot safely resume the primary after the child.
func validateFabricContinuationCapability(servingProtocol string, call modelTurnToolCall, terminal map[string]any, nativeResponseID string) error {
	_, err := planFabricContinuation(servingProtocol, call, terminal, nativeResponseID)
	return err
}

// childToolResultPayload builds the memory-only structured tool-result body.
// Truncation is never plain OutputText with IsError=false.
func childToolResultPayload(childOut modelTurnResult, childFailed bool, fallbackReason string) (text string, isError bool) {
	status := "completed"
	if childFailed {
		status = "failed"
		isError = true
	}
	output := childOut.OutputText
	if childFailed && strings.TrimSpace(output) == "" {
		output = fallbackReason
	}
	if childOut.Truncated {
		body := map[string]any{
			"status":    status,
			"truncated": true,
		}
		if childFailed {
			body["error"] = output
		} else {
			body["output"] = output
		}
		b, err := json.Marshal(body)
		if err != nil {
			return output, isError
		}
		return string(b), isError
	}
	return output, isError
}

// fabricContinuationVisibleBytes estimates model-visible continuation payload bytes
// for ClassContinuation reservation (messages JSON; fail closed on reserve error).
func fabricContinuationVisibleBytes(parsed protocol.ParsedRequest) int64 {
	b, err := json.Marshal(parsed.Context.Messages)
	if err != nil {
		// Fail closed with a non-zero lower bound so Reserve still gates capacity.
		n := 0
		for _, msg := range parsed.Context.Messages {
			for _, part := range msg.Content {
				n += len(part.Text)
			}
			if msg.ToolCallID != "" {
				n += len(msg.ToolCallID)
			}
			if msg.ToolName != "" {
				n += len(msg.ToolName)
			}
		}
		if n <= 0 {
			n = 1
		}
		return int64(n)
	}
	if len(b) == 0 {
		return 1
	}
	return int64(len(b))
}

// driveWithOptionalHandoff runs the primary turn and, when delegation.model is set
// and the primary emits the reserved delegate tool, performs exactly one child
// handoff then a structured tool-result continuation. Three runModelTurn calls
// mint three distinct req_* ids under one primary runId.
func (rt *fabricRuntime) driveWithOptionalHandoff(ctx context.Context, repo *fabric.Repo, live *fabricLiveRun, model, input, delegationModel string) {
	defer close(live.done)
	defer func() {
		if live.turn != nil {
			_ = live.turn.Close()
			live.turn = nil
		}
		rt.mu.Lock()
		claim := live.claimSnapshot()
		if cur, ok := rt.active[claim.TaskID]; ok && cur.claimSnapshot().RunID == claim.RunID {
			delete(rt.active, claim.TaskID)
		}
		rt.inflight--
		rt.mu.Unlock()
	}()

	_ = repo.RecordProgress(live.claimSnapshot(), "routing")

	primaryParsed := (*protocol.ParsedRequest)(nil)
	if strings.TrimSpace(delegationModel) != "" {
		inputText := strings.TrimSpace(input)
		if inputText == "" {
			inputText = "proceed"
		}
		req := protocol.ParsedRequest{
			Source:  protocol.RequestSourceResponses,
			ModelID: model,
			Stream:  false,
			Context: protocol.Context{
				Messages: []protocol.Message{{
					Role:    protocol.RoleUser,
					Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: inputText}},
				}},
				Tools: []protocol.Tool{fabricDelegateTool()},
			},
			Options: protocol.RequestOptions{
				ToolChoice:        &protocol.ToolChoice{Kind: protocol.ToolChoiceAuto},
				ParallelToolCalls: boolPtr(false),
			},
		}
		primaryParsed = &req
	}

	claim := live.claimSnapshot()
	outcome, err := rt.h.runModelTurn(ctx, modelTurnInput{
		Model:     model,
		Input:     input,
		Parsed:    primaryParsed,
		Surface:   "",
		RequestID: live.requestID,
		Turn:      live.turn,
		Path:      "/api/fabric/tasks/" + claim.TaskID + "/execute",
		Protocol:  "fabric-execute",
		Persist:   true,
	})

	if err == nil && outcome.Status == "completed" && strings.TrimSpace(delegationModel) != "" {
		call, decision, rejectReason := decideFabricPrimaryToolCalls(outcome.ToolCalls)
		switch decision {
		case fabricPrimaryOneDelegate:
			rt.runSingleChildHandoff(ctx, repo, live, model, input, delegationModel, call, outcome)
			return
		case fabricPrimaryReject:
			// Fail closed BEFORE BeginChildHandoff: no child, no continuation.
			if live.turn != nil {
				_ = live.turn.Close()
				live.turn = nil
			}
			if live.selectTerminal(fabricIntentFail) {
				rt.persistFail(repo, live, rejectReason)
			}
			return
		}
	}

	// Non-handoff path: release the primary admission turn, then finish.
	if live.turn != nil {
		_ = live.turn.Close()
		live.turn = nil
	}
	rt.finishPrimaryDrive(ctx, repo, live, outcome, err)
}

func (rt *fabricRuntime) runSingleChildHandoff(ctx context.Context, repo *fabric.Repo, live *fabricLiveRun, primaryModel, input, delegationModel string, call modelTurnToolCall, primaryOut modelTurnResult) {
	var childTurn *resourcebudget.Turn
	closeChildTurn := func() {
		if childTurn != nil {
			_ = childTurn.Close()
			childTurn = nil
		}
	}
	defer closeChildTurn()

	instruction, parseErr := parseDelegateInstruction(call.Arguments)
	if parseErr != nil {
		if live.turn != nil {
			_ = live.turn.Close()
			live.turn = nil
		}
		if live.selectTerminal(fabricIntentFail) {
			rt.persistFail(repo, live, clipFabricReason(parseErr.Error()))
		}
		return
	}

	// Fail closed BEFORE durable child handoff when continuation cannot be reconstructed
	// under existing provider mechanisms (ThoughtSignature / previous_response binding).
	plan, cerr := planFabricContinuation(primaryOut.ServingProtocol, call, primaryOut.TerminalResponse, primaryOut.NativeResponseID)
	if cerr != nil {
		if live.turn != nil {
			_ = live.turn.Close()
			live.turn = nil
		}
		if live.selectTerminal(fabricIntentFail) {
			rt.persistFail(repo, live, clipFabricReason(cerr.Error()))
		}
		return
	}
	contPlan := plan

	// 1. Observe cancel/shutdown terminal intent first (cancel-first).
	switch live.selectedTerminal() {
	case fabricIntentShutdown:
		if live.turn != nil {
			_ = live.turn.Close()
			live.turn = nil
		}
		rt.persistInterrupt(repo, live, "shutdown")
		return
	case fabricIntentCancel:
		if live.turn != nil {
			_ = live.turn.Close()
			live.turn = nil
		}
		rt.persistCancel(repo, live, "operator_cancelled")
		return
	}

	primaryClaim := live.claimSnapshot()
	primaryOwner := primaryClaim.Owner

	// 2. Close/release completed parent resource Turn BEFORE child acquire.
	if live.turn != nil {
		_ = live.turn.Close()
		live.turn = nil
	}

	// 3-5. TryAcquire child capacity before parent->child CAS. Never block forever.
	if rt.h != nil && rt.h.resourceBudget != nil {
		var ok bool
		childTurn, ok = rt.h.resourceBudget.TryAcquireTurn("")
		if !ok {
			if live.selectTerminal(fabricIntentFail) {
				rt.persistFail(repo, live, "child_capacity_exceeded")
			}
			return
		}
		if _, err := childTurn.Reserve(resourcebudget.ClassRequestBody, int64(len(instruction))); err != nil {
			closeChildTurn()
			if live.selectTerminal(fabricIntentFail) {
				rt.persistFail(repo, live, "child_capacity_exceeded")
			}
			return
		}
		childTurn.SetPhase(resourcebudget.PhaseAdmission)
	}

	// Test hook: cancel-wins acquires authority before handoff enters the boundary.
	if hook := fabricTestBeforeAuthorityHandoff; hook != nil {
		hook()
	}

	// Parent->child under live.authorityMu: re-check terminal intent, capture claim,
	// BeginChildHandoff, publish child claim, then unlock.
	live.authorityMu.Lock()
	switch live.selectedTerminal() {
	case fabricIntentShutdown:
		live.authorityMu.Unlock()
		closeChildTurn()
		rt.persistInterrupt(repo, live, "shutdown")
		return
	case fabricIntentCancel:
		live.authorityMu.Unlock()
		closeChildTurn()
		rt.persistCancel(repo, live, "operator_cancelled")
		return
	}
	primaryClaim = live.claimSnapshot()
	primaryOwner = primaryClaim.Owner
	child, err := repo.BeginChildHandoff(primaryClaim, delegationModel)
	if err != nil {
		live.authorityMu.Unlock()
		closeChildTurn()
		// Post-CAS storage uncertainty must always be reconciled in-process, even
		// when another terminal intent (shutdown) already won selectTerminal.
		reason := clipFabricReason(err.Error())
		if rt.reconcileHandoffStorageFailure(repo, live, primaryClaim.RunID, "handoff_storage_failed:"+reason) {
			return
		}
		if live.selectTerminal(fabricIntentFail) {
			rt.persistFail(repo, live, reason)
		}
		return
	}
	if hook := fabricTestAfterDurableHandoffBeforePublish; hook != nil {
		hook()
	}
	live.setClaimOwnerFence(child.Owner, child.Fence)
	switch live.selectedTerminal() {
	case fabricIntentShutdown:
		live.authorityMu.Unlock()
		closeChildTurn()
		rt.applyChildReturnTerminal(repo, live, child, primaryOwner, fabricIntentShutdown)
		return
	case fabricIntentCancel:
		live.authorityMu.Unlock()
		closeChildTurn()
		rt.applyChildReturnTerminal(repo, live, child, primaryOwner, fabricIntentCancel)
		return
	}
	live.authorityMu.Unlock()

	childReqID := sessions.NewRequestID()
	_ = repo.RecordProgress(live.claimSnapshot(), "child")
	childOut, childErr := rt.h.runModelTurn(ctx, modelTurnInput{
		Model:     delegationModel,
		Input:     instruction,
		Surface:   "",
		RequestID: childReqID,
		Turn:      childTurn,
		Path:      "/api/fabric/tasks/" + primaryClaim.TaskID + "/execute",
		Protocol:  "fabric-execute-child",
		Persist:   true,
	})

	// 7. Close child Turn EXACTLY ONCE immediately after child model turn finished.
	closeChildTurn()

	// Honor cancel/shutdown that won while the child ran.
	switch live.selectedTerminal() {
	case fabricIntentShutdown:
		rt.applyChildReturnTerminal(repo, live, child, primaryOwner, fabricIntentShutdown)
		return
	case fabricIntentCancel:
		rt.applyChildReturnTerminal(repo, live, child, primaryOwner, fabricIntentCancel)
		return
	}

	var returned fabric.RunIdentity
	childFailed := childErr != nil || childOut.Status != "completed"
	fallbackReason := firstNonEmptyReason(childOut.Reason, "child_failed")
	if childErr != nil {
		fallbackReason = clipFabricReason(childErr.Error())
	}
	childOutput, toolErr := childToolResultPayload(childOut, childFailed, fallbackReason)

	if hook := fabricTestBeforeAuthorityReturn; hook != nil {
		hook()
	}

	// Child->parent return under the same authority boundary as cancel.
	live.authorityMu.Lock()
	switch live.selectedTerminal() {
	case fabricIntentShutdown:
		live.authorityMu.Unlock()
		rt.applyChildReturnTerminal(repo, live, child, primaryOwner, fabricIntentShutdown)
		return
	case fabricIntentCancel:
		live.authorityMu.Unlock()
		rt.applyChildReturnTerminal(repo, live, child, primaryOwner, fabricIntentCancel)
		return
	}
	if childFailed {
		returned, err = repo.FailChildHandoff(child, primaryOwner, fallbackReason)
	} else {
		returned, err = repo.CompleteChildHandoff(child, primaryOwner)
	}
	if err != nil {
		live.authorityMu.Unlock()
		// Lease may already be at parent N+2; do not persistFail with a stale child claim.
		live.setClaimOwnerFence(child.Owner, child.Fence)
		// Post-CAS return storage uncertainty must always be reconciled in-process,
		// even when shutdown already won selectTerminal.
		reason := clipFabricReason(err.Error())
		if rt.reconcileHandoffStorageFailure(repo, live, child.RunID, "handoff_return_storage_failed:"+reason) {
			return
		}
		if live.selectTerminal(fabricIntentFail) {
			rt.persistFail(repo, live, reason)
		}
		return
	}
	if hook := fabricTestAfterDurableReturnBeforePublish; hook != nil {
		hook()
	}
	live.replaceClaim(returned)
	live.authorityMu.Unlock()

	if errorsIsCanceled(ctx) {
		if rt.shuttingDownLocked() {
			if live.selectTerminal(fabricIntentShutdown) {
				rt.persistInterrupt(repo, live, "shutdown")
			}
			return
		}
		if live.selectTerminal(fabricIntentCancel) {
			rt.persistCancel(repo, live, "cancelled")
		}
		return
	}

	// 8. After authority return: TryAcquire resume Turn (never AcquireTurn forever).
	var resumeTurn *resourcebudget.Turn
	if rt.h != nil && rt.h.resourceBudget != nil {
		var ok bool
		resumeTurn, ok = rt.h.resourceBudget.TryAcquireTurn("")
		if !ok {
			if live.selectTerminal(fabricIntentFail) {
				rt.persistFail(repo, live, "resume_capacity_exceeded")
			}
			return
		}
		resumeTurn.SetPhase(resourcebudget.PhaseAdmission)
		live.turn = resumeTurn
	}

	contReqID := sessions.NewRequestID()
	parsed := buildFabricContinuationParsed(primaryModel, input, call, childOutput, toolErr, contPlan)
	if resumeTurn != nil {
		contBytes := fabricContinuationVisibleBytes(parsed)
		if _, rerr := resumeTurn.Reserve(resourcebudget.ClassContinuation, contBytes); rerr != nil {
			_ = resumeTurn.Close()
			live.turn = nil
			if live.selectTerminal(fabricIntentFail) {
				rt.persistFail(repo, live, "continuation_capacity_exceeded")
			}
			return
		}
	}
	claim := live.claimSnapshot()
	contIn := modelTurnInput{
		Model:     primaryModel,
		Parsed:    &parsed,
		Surface:   "",
		RequestID: contReqID,
		Turn:      resumeTurn,
		Path:      "/api/fabric/tasks/" + claim.TaskID + "/execute",
		Protocol:  "fabric-execute-continue",
		Persist:   true,
	}
	// Pin Fabric continuation to the primary turn's committed physical provider.
	// Do not re-resolve combo/policy routes (would restart failover with B's native state on A).
	if primaryOut.Resolved != nil && primaryOut.Resolved.Provider != nil {
		contIn.PreResolved = primaryOut.Resolved
		contIn.PreferCommitted = true
		if primaryOut.PhysicalPin != nil {
			pin := *primaryOut.PhysicalPin
			contIn.PhysicalPin = &pin
		}
		if primaryOut.Route.Provider != "" || primaryOut.Route.Model != "" {
			routeCopy := primaryOut.Route
			contIn.PreResolvedRoute = &routeCopy
		}
	}
	contOut, contErr := rt.h.runModelTurn(ctx, contIn)
	rt.finishPrimaryDrive(ctx, repo, live, contOut, contErr)
}

// applyChildReturnTerminal handles Cancel/Interrupt child-to-parent return.
// On success it publishes the returned parent N+2 claim, then persists the matching
// primary terminal. On error it treats authority as uncertain (return CAS may already
// have moved the lease) and reconciles in-process BEFORE any persistence that would
// use a possibly-stale child N+1 live claim. There is no CAS rollback; monotonic
// fencing is preserved. If reconciliation cannot persist, this returns without
// claiming convergence (dual-storage-failure limitation).
func (rt *fabricRuntime) applyChildReturnTerminal(repo *fabric.Repo, live *fabricLiveRun, child fabric.ChildRunIdentity, primaryOwner string, intent fabricTerminalIntent) {
	if rt == nil || repo == nil || live == nil {
		return
	}
	var (
		returned fabric.RunIdentity
		herr     error
		reason   string
	)
	switch intent {
	case fabricIntentShutdown:
		reason = "shutdown"
		returned, herr = repo.InterruptChildHandoff(child, primaryOwner, reason)
	case fabricIntentCancel:
		reason = "operator_cancelled"
		returned, herr = repo.CancelChildHandoff(child, primaryOwner, reason)
	default:
		return
	}
	if herr == nil {
		live.replaceClaim(returned)
		switch intent {
		case fabricIntentShutdown:
			rt.persistInterrupt(repo, live, reason)
		case fabricIntentCancel:
			rt.persistCancel(repo, live, reason)
		}
		return
	}
	// Post-CAS return storage uncertainty: never persistCancel/Interrupt with a
	// stale child claim (fencing fail → live.done → false HTTP/Close success).
	// Propagate reconcile outcome onto live.durableConverged so cancelCurrent/
	// cancelExpected do not report HTTP success when BOTH return HandoffCommitted
	// and ReconcileExecuteInterrupted fail to persist.
	_ = rt.reconcileHandoffStorageFailure(repo, live, child.RunID, "handoff_return_storage_failed:"+clipFabricReason(herr.Error()))
}

func (rt *fabricRuntime) syncPrimaryClaim(repo *fabric.Repo, live *fabricLiveRun, primaryOwner string) {
	if live == nil {
		return
	}
	live.setClaimOwnerFence(primaryOwner, live.claimSnapshot().Fence)
	if repo == nil {
		return
	}
	if _, tok, err := repo.LeaseSnapshot(live.claimSnapshot().TaskID); err == nil {
		live.setClaimOwnerFence(primaryOwner, tok)
	}
}

func (rt *fabricRuntime) finishPrimaryDrive(ctx context.Context, repo *fabric.Repo, live *fabricLiveRun, outcome modelTurnResult, err error) {
	claim := live.claimSnapshot()
	if err == nil && outcome.Status == "completed" && live.selectTerminal(fabricIntentComplete) {
		if persistErr := repo.CompleteExecute(claim); persistErr != nil {
			_ = rt.reconcileTerminalPersist(repo, live, persistErr)
			return
		}
		live.markDurableConverged()
		if rt.results != nil {
			rt.results.put(fabricStoredResult{
				Handle:    "fr_" + claim.RunID,
				RunID:     claim.RunID,
				TaskID:    claim.TaskID,
				Status:    "completed",
				Output:    outcome.OutputText,
				Provider:  outcome.Provider,
				Model:     outcome.ResolvedModel,
				RequestID: outcome.RequestID,
				Truncated: outcome.Truncated,
			})
		}
		return
	}

	switch live.selectedTerminal() {
	case fabricIntentComplete:
		return
	case fabricIntentShutdown:
		rt.persistInterrupt(repo, live, "shutdown")
		return
	case fabricIntentCancel:
		rt.persistCancel(repo, live, "operator_cancelled")
		return
	}

	if errorsIsCanceled(ctx) && rt.shuttingDownLocked() {
		if live.selectTerminal(fabricIntentShutdown) {
			rt.persistInterrupt(repo, live, "shutdown")
			return
		}
	}
	switch live.selectedTerminal() {
	case fabricIntentShutdown:
		rt.persistInterrupt(repo, live, "shutdown")
		return
	case fabricIntentCancel:
		rt.persistCancel(repo, live, "operator_cancelled")
		return
	case fabricIntentComplete, fabricIntentFail:
		return
	}

	if err != nil {
		if errorsIsCanceled(ctx) {
			if live.selectTerminal(fabricIntentCancel) {
				rt.persistCancel(repo, live, "cancelled")
			}
			return
		}
		if live.selectTerminal(fabricIntentFail) {
			rt.persistFail(repo, live, clipFabricReason(err.Error()))
		}
		return
	}
	switch outcome.Status {
	case "failed":
		if live.selectTerminal(fabricIntentFail) {
			rt.persistFail(repo, live, outcome.Reason)
		}
	case "cancelled":
		if live.selectTerminal(fabricIntentCancel) {
			rt.persistCancel(repo, live, firstNonEmptyReason(outcome.Reason, "cancelled"))
		}
	case "interrupted":
		if live.selectTerminal(fabricIntentShutdown) {
			rt.persistInterrupt(repo, live, outcome.Reason)
		}
	default:
		if live.selectTerminal(fabricIntentFail) {
			rt.persistFail(repo, live, "unknown_turn_status")
		}
	}
}

func errorsIsCanceled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.Canceled) || ctx.Err() != nil
}

func boolPtr(v bool) *bool { return &v }
