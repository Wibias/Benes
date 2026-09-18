package cursor

import "github.com/Wibias/Benes/internal/protocol"

func RecomputeEstimatedTotal(input, output, cached int64) int64 {
	if input < 0 {
		input = 0
	}
	if output < 0 {
		output = 0
	}
	_ = cached
	// Cursor continuation is checkpoint state, not cache_read_tokens.
	return input + output
}

func NormalizeCursorUsage(estimate, providerInput, output, window int64) protocol.Usage {
	if output < 0 {
		output = 0
	}
	usage := protocol.Usage{OutputTokens: output}
	if providerInput > 0 {
		usage.InputTokens = providerInput
		if window > 0 && usage.InputTokens > window {
			usage.InputTokens = window
		}
		usage.TotalTokens = RecomputeEstimatedTotal(usage.InputTokens, output, 0)
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
	usage.TotalTokens = RecomputeEstimatedTotal(input, output, 0)
	usage.Estimated = true
	return usage
}
