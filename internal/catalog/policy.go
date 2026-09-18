package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

var (
	ErrInvalidNativeModelID = errors.New("invalid native OpenAI model id")
	ErrEmptyCombo           = errors.New("combo requires at least one member")
)

func PolicyFromRoot(raw json.RawMessage) Policy {
	var root struct {
		RetainModels                map[string]map[string]bool               `json:"retainModels"`
		ProviderContextCaps         map[string]int                           `json:"providerContextCaps"`
		ProviderContextCapDisabled  map[string]bool                          `json:"providerContextCapDisabled"`
		ModelContextWindows         map[string]map[string]ContextPolicyValue `json:"modelContextWindows"`
		PreserveExactReasoningRungs map[string]bool                          `json:"preserveExactReasoningRungs"`
		AutoCompactTokenLimits      map[string]map[string]int                `json:"autoCompactTokenLimits"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return Policy{}
	}
	return Policy{
		RetainModels:                root.RetainModels,
		ProviderContextCaps:         root.ProviderContextCaps,
		ProviderContextCapDisabled:  root.ProviderContextCapDisabled,
		ModelContextWindows:         root.ModelContextWindows,
		PreserveExactReasoningRungs: root.PreserveExactReasoningRungs,
		AutoCompactTokenLimits:      root.AutoCompactTokenLimits,
	}
}

type PolicyStore struct {
	Transactions         *config.TransactionStore
	NativeOpenAIModelIDs map[string]struct{}
}

type PolicyTransaction struct {
	store *PolicyStore
	tx    *config.Transaction
}

func (s PolicyStore) Begin() (*PolicyTransaction, error) {
	if s.Transactions == nil {
		return nil, fmt.Errorf("catalog policy transaction store is required")
	}
	tx, err := s.Transactions.Begin()
	if err != nil {
		return nil, err
	}
	return &PolicyTransaction{store: &s, tx: tx}, nil
}

func (tx *PolicyTransaction) SetContextWindow(providerID, modelID string, tokens int, mode WindowMode) error {
	if err := tx.validateProviderModel(providerID, modelID); err != nil {
		return err
	}
	if tokens <= 0 {
		return fmt.Errorf("context window must be positive")
	}
	if mode != WindowFallback && mode != WindowOverride {
		return fmt.Errorf("invalid context window mode %q", mode)
	}
	value, err := json.Marshal(ContextPolicyValue{Tokens: tokens, Mode: mode})
	if err != nil {
		return err
	}
	return tx.tx.Set(config.JSONPath("modelContextWindows", providerID, modelID), value)
}

func (tx *PolicyTransaction) ClearContextWindow(providerID, modelID string) error {
	if err := tx.validateProviderModel(providerID, modelID); err != nil {
		return err
	}
	return tx.tx.Delete(config.JSONPath("modelContextWindows", providerID, modelID))
}

func (tx *PolicyTransaction) SetAutoCompactLimit(providerID, modelID string, tokens int) error {
	if err := tx.validateProviderModel(providerID, modelID); err != nil {
		return err
	}
	if tokens <= 0 {
		return fmt.Errorf("auto compact token limit must be positive")
	}
	value, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	return tx.tx.Set(config.JSONPath("autoCompactTokenLimits", providerID, modelID), value)
}

func (tx *PolicyTransaction) ClearAutoCompactLimit(providerID, modelID string) error {
	if err := tx.validateProviderModel(providerID, modelID); err != nil {
		return err
	}
	return tx.tx.Delete(config.JSONPath("autoCompactTokenLimits", providerID, modelID))
}

func (tx *PolicyTransaction) SetRetainModel(providerID, modelID string, retain bool) error {
	if err := tx.validateProviderModel(providerID, modelID); err != nil {
		return err
	}
	path := config.JSONPath("retainModels", providerID, modelID)
	if !retain {
		return tx.tx.Delete(path)
	}
	return tx.tx.Set(path, json.RawMessage("true"))
}

func (tx *PolicyTransaction) SetPreserveExactReasoning(providerID, modelID string, preserve bool) error {
	if err := tx.validateProviderModel(providerID, modelID); err != nil {
		return err
	}
	value, err := json.Marshal(preserve)
	if err != nil {
		return err
	}
	return tx.tx.Set(config.JSONPath("preserveExactReasoningRungs", providerID, modelID), value)
}

func (tx *PolicyTransaction) Commit() (config.Revision, error) {
	if tx == nil || tx.tx == nil {
		return "", fmt.Errorf("catalog policy transaction is nil")
	}
	return tx.tx.Commit()
}

func (tx *PolicyTransaction) validateProviderModel(providerID, modelID string) error {
	if tx == nil || tx.tx == nil || tx.store == nil {
		return fmt.Errorf("catalog policy transaction is nil")
	}
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" || strings.Contains(modelID, "/") && providerID == "openai" {
		if providerID == "openai" {
			return ErrInvalidNativeModelID
		}
		return fmt.Errorf("provider and model ids are required")
	}
	if providerID == "openai" && tx.store.NativeOpenAIModelIDs != nil {
		if _, ok := tx.store.NativeOpenAIModelIDs[modelID]; !ok {
			return ErrInvalidNativeModelID
		}
	}
	return nil
}
