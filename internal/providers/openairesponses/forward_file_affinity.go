package openairesponses

import (
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

func canReplayForwardAccountBoundFiles(request protocol.ParsedRequest, current, next ForwardCredential) bool {
	if !hasForwardAccountBoundFileReference(request) {
		return true
	}
	return sameForwardPhysicalAccount(current, next)
}

func hasForwardAccountBoundFileReference(request protocol.ParsedRequest) bool {
	for _, message := range request.Context.Messages {
		for _, part := range message.Content {
			if strings.TrimSpace(part.FileID) != "" {
				return true
			}
		}
	}
	return false
}

func sameForwardPhysicalAccount(current, next ForwardCredential) bool {
	currentTrusted := strings.TrimSpace(current.TrustedAccountID)
	nextTrusted := strings.TrimSpace(next.TrustedAccountID)
	if currentTrusted != "" || nextTrusted != "" {
		return currentTrusted != "" && currentTrusted == nextTrusted
	}

	currentChatGPT := strings.TrimSpace(current.ChatGPTAccountID)
	nextChatGPT := strings.TrimSpace(next.ChatGPTAccountID)
	if currentChatGPT != "" || nextChatGPT != "" {
		return currentChatGPT != "" && currentChatGPT == nextChatGPT
	}

	currentAuth := strings.TrimSpace(current.Authorization)
	nextAuth := strings.TrimSpace(next.Authorization)
	return currentAuth != "" && currentAuth == nextAuth
}
