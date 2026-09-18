package codexauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

type ManagedAccount struct {
	ID               string
	Email            string
	Alias            string
	Plan             string
	ChatGPTAccountID string
	LogLabel         string
	IsMain           bool
}

type ManagedAccountConfig struct {
	Accounts         []ManagedAccount
	PausedAccountIDs map[string]bool
	Priorities       map[string]int
	ActiveAccountID  string
	PinnedAccountID  string
}

func ProjectManagedAccountConfig(raw []byte) (ManagedAccountConfig, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		if err == nil {
			err = fmt.Errorf("root must be an object")
		}
		return ManagedAccountConfig{}, fmt.Errorf("decode Codex account config: %w", err)
	}

	result := ManagedAccountConfig{
		PausedAccountIDs: make(map[string]bool),
		Priorities:       make(map[string]int),
	}

	if value, ok := root["codexAccounts"]; ok {
		var rows []json.RawMessage
		if isJSONNullBytes(value) || json.Unmarshal(value, &rows) != nil || rows == nil {
			return ManagedAccountConfig{}, fmt.Errorf("decode Codex account config: codexAccounts must be an array")
		}
		result.Accounts = make([]ManagedAccount, 0, len(rows))
		for index, row := range rows {
			account, err := decodeManagedAccount(row)
			if err != nil {
				return ManagedAccountConfig{}, fmt.Errorf("decode Codex account config: codexAccounts[%d]: %w", index, err)
			}
			result.Accounts = append(result.Accounts, account)
		}
	}

	if value, ok := root["pausedCodexAccountIds"]; ok {
		var rows []json.RawMessage
		if isJSONNullBytes(value) || json.Unmarshal(value, &rows) != nil || rows == nil {
			return ManagedAccountConfig{}, fmt.Errorf("decode Codex account config: pausedCodexAccountIds must be an array")
		}
		for index, row := range rows {
			var id string
			if json.Unmarshal(row, &id) != nil {
				return ManagedAccountConfig{}, fmt.Errorf("decode Codex account config: pausedCodexAccountIds[%d] must be a string", index)
			}
			result.PausedAccountIDs[id] = true
		}
	}

	if value, ok := root["codexAccountPriorities"]; ok {
		var entries map[string]json.RawMessage
		if isJSONNullBytes(value) || json.Unmarshal(value, &entries) != nil || entries == nil {
			return ManagedAccountConfig{}, fmt.Errorf("decode Codex account config: codexAccountPriorities must be an object")
		}
		for id, rawPriority := range entries {
			var priority float64
			if json.Unmarshal(rawPriority, &priority) != nil || math.Trunc(priority) != priority || priority < -100 || priority > 100 {
				return ManagedAccountConfig{}, fmt.Errorf("decode Codex account config: priority for %q must be an integer from -100 to 100", id)
			}
			result.Priorities[id] = int(priority)
		}
	}

	var err error
	if result.ActiveAccountID, err = optionalStringField(root, "activeCodexAccountId"); err != nil {
		return ManagedAccountConfig{}, err
	}
	if result.PinnedAccountID, err = optionalStringField(root, "activeCodexAccountPinned"); err != nil {
		return ManagedAccountConfig{}, err
	}
	return result, nil
}

func decodeManagedAccount(raw json.RawMessage) (ManagedAccount, error) {
	var row map[string]json.RawMessage
	if json.Unmarshal(raw, &row) != nil || row == nil {
		return ManagedAccount{}, fmt.Errorf("account must be an object")
	}

	var account ManagedAccount
	if value, ok := row["id"]; !ok || json.Unmarshal(value, &account.ID) != nil {
		return ManagedAccount{}, fmt.Errorf("id must be a string")
	}

	var err error
	for key, destination := range map[string]*string{
		"email":            &account.Email,
		"alias":            &account.Alias,
		"plan":             &account.Plan,
		"chatgptAccountId": &account.ChatGPTAccountID,
		"logLabel":         &account.LogLabel,
	} {
		*destination, err = optionalStringField(row, key)
		if err != nil {
			return ManagedAccount{}, err
		}
	}
	if value, ok := row["isMain"]; ok {
		if json.Unmarshal(value, &account.IsMain) != nil {
			return ManagedAccount{}, fmt.Errorf("isMain must be a boolean")
		}
	}
	return account, nil
}

func optionalStringField(root map[string]json.RawMessage, key string) (string, error) {
	value, ok := root[key]
	if !ok {
		return "", nil
	}
	var result string
	if isJSONNullBytes(value) || json.Unmarshal(value, &result) != nil {
		return "", fmt.Errorf("decode Codex account config: %s must be a string", key)
	}
	return result, nil
}

func isJSONNullBytes(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
