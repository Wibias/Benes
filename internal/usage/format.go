package usage

import (
	"fmt"
	"strings"
	"time"
)

func FormatMarkdown(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Usage (%s, %s)\n\n", s.Range, s.Surface)
	if s.Since != nil {
		fmt.Fprintf(&b, "- since: %s\n", time.UnixMilli(*s.Since).UTC().Format(time.RFC3339))
	}
	if s.Until != nil {
		fmt.Fprintf(&b, "- until: %s\n", time.UnixMilli(*s.Until).UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "- requests: %d\n", s.Summary.Requests)
	fmt.Fprintf(&b, "- tokens: %d\n", s.Summary.TotalTokens)
	fmt.Fprintf(&b, "- estimated cost USD: %.4f\n\n", s.Summary.EstimatedCostUsd)
	fmt.Fprintf(&b, "| Date | Requests | Tokens |\n| --- | --- | --- |\n")
	if len(s.Days) == 0 {
		fmt.Fprintf(&b, "| - | %d | %d |\n", s.Summary.Requests, s.Summary.TotalTokens)
	} else {
		for _, day := range s.Days {
			fmt.Fprintf(&b, "| %s | %d | %d |\n", day.Date, day.Requests, day.TotalTokens)
		}
	}
	if len(s.Models) > 0 {
		fmt.Fprintf(&b, "\n| Model | Requests | Tokens |\n| --- | --- | --- |\n")
		for _, row := range s.Models {
			fmt.Fprintf(&b, "| %s/%s | %d | %d |\n", row.Provider, row.Model, row.Requests, row.TotalTokens)
		}
	}
	if len(s.Providers) > 0 {
		fmt.Fprintf(&b, "\n| Provider | Requests | Tokens |\n| --- | --- | --- |\n")
		for _, row := range s.Providers {
			fmt.Fprintf(&b, "| %s | %d | %d |\n", row.Provider, row.Requests, row.TotalTokens)
		}
	}
	return b.String()
}
