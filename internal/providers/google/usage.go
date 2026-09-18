package google

import "github.com/Wibias/Benes/internal/protocol"

func NormalizeUsage(estimate, providerInput, output, cached, window int64) protocol.Usage {
	if output < 0 {
		output = 0
	}
	if cached < 0 {
		cached = 0
	}
	usage := protocol.Usage{OutputTokens: output}
	if cached > 0 {
		usage.CachedInputTokens = cached
	}
	if providerInput > 0 {
		usage.InputTokens = providerInput
		if window > 0 && usage.InputTokens > window {
			usage.InputTokens = window
		}
		usage.TotalTokens = usage.InputTokens + output
		return usage
	}
	input := estimate
	if input < 0 {
		input = 0
	}
	if window > 0 && input > window {
		input = window
	}
	usage.InputTokens = input
	usage.TotalTokens = input + output
	usage.Estimated = true
	return usage
}
