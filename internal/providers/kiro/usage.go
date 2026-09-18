package kiro

import (
	"math"

	"github.com/Wibias/Benes/internal/protocol"
)

func OccupancyTokens(percent float64, window int64) int64 {
	if percent <= 0 || window <= 0 || math.IsNaN(percent) || math.IsInf(percent, 0) {
		return 0
	}
	if percent > 100 {
		percent = 100
	}
	return int64(math.Round(percent / 100 * float64(window)))
}

func ApplyKiroOccupancy(usage protocol.Usage, percent float64, window int64) protocol.Usage {
	occupancy := OccupancyTokens(percent, window)
	if occupancy <= 0 {
		return usage
	}
	usage.ContextTotalTokens = occupancy
	return usage
}

func NormalizeKiroUsage(estimate, providerInput, output, window int64) protocol.Usage {
	if output < 0 {
		output = 0
	}
	usage := protocol.Usage{OutputTokens: output}
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
