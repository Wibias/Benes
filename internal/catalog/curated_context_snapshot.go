// Code generated from models.dev metadata revision 6303a062a3782391f3147e9db1100ee93139abbb.
// Source repository: anomalyco/models.dev. DO NOT EDIT MANUALLY.
package catalog

import "strings"

const curatedContextMetadataRevision = "6303a062a3782391f3147e9db1100ee93139abbb"

type curatedContextRow struct {
	Context   int
	MaxOutput int
	Path      string
}

var curatedContextRows = map[string]curatedContextRow{
	"opencode-go/deepseek-v4-flash-vision-exp": {Context: 1_000_000, MaxOutput: 384_000, Path: "models/deepseek/deepseek-v4-flash-vision-exp.toml"},
	"opencode-go/deepseek-v4-flash":            {Context: 1_000_000, MaxOutput: 384_000, Path: "models/deepseek/deepseek-v4-flash-0731.toml"},
	"opencode-go/deepseek-v4-pro":              {Context: 1_000_000, MaxOutput: 384_000, Path: "providers/opencode-go/models/deepseek-v4-pro.toml"},
	"opencode-go/deepseek-v4.1-flash":          {Context: 1_000_000, MaxOutput: 384_000, Path: "models/deepseek/deepseek-v4.1-flash.toml"},
	"opencode-go/glm-5.1":                      {Context: 202_752, MaxOutput: 32_768, Path: "providers/opencode-go/models/glm-5.1.toml"},
	"opencode-go/glm-5.2":                      {Context: 1_000_000, MaxOutput: 131_072, Path: "models/zhipuai/glm-5.2.toml"},
	"opencode-go/glm-5.3-flash":                {Context: 1_000_000, MaxOutput: 131_072, Path: "models/zhipuai/glm-5.3-flash.toml"},
	"opencode-go/glm-5.3":                      {Context: 1_000_000, MaxOutput: 131_072, Path: "models/zhipuai/glm-5.3.toml"},
	"opencode-go/glm-5":                        {Context: 202_752, MaxOutput: 32_768, Path: "providers/opencode-go/models/glm-5.toml"},
	"opencode-go/gpt-5.6-luna":                 {Context: 1_050_000, MaxOutput: 128_000, Path: "models/openai/gpt-5.6-luna.toml"},
	"opencode-go/grok-4.5":                     {Context: 500_000, MaxOutput: 500_000, Path: "models/xai/grok-4.5.toml"},
	"opencode-go/grok-4.6":                     {Context: 500_000, MaxOutput: 500_000, Path: "models/xai/grok-4.6.toml"},
	"opencode-go/hy3":                          {Context: 256_000, MaxOutput: 128_000, Path: "models/tencent/hy3.toml"},
	"opencode-go/hy4-preview":                  {Context: 1_024_000, MaxOutput: 64_000, Path: "models/tencent/hy4-preview.toml"},
	"opencode-go/kimi-k2.5":                    {Context: 262_144, MaxOutput: 65_536, Path: "providers/opencode-go/models/kimi-k2.5.toml"},
	"opencode-go/kimi-k2.6":                    {Context: 262_144, MaxOutput: 65_536, Path: "providers/opencode-go/models/kimi-k2.6.toml"},
	"opencode-go/kimi-k2.7-code":               {Context: 262_144, MaxOutput: 262_144, Path: "models/moonshotai/kimi-k2.7-code.toml"},
	"opencode-go/kimi-k3":                      {Context: 1_048_576, MaxOutput: 131_072, Path: "models/moonshotai/kimi-k3.toml"},
	"opencode-go/longcat-2.0":                  {Context: 1_000_000, MaxOutput: 131_072, Path: "models/meituan/longcat-2.0.toml"},
	"opencode-go/mimo-v2-omni":                 {Context: 262_144, MaxOutput: 128_000, Path: "providers/opencode-go/models/mimo-v2-omni.toml"},
	"opencode-go/mimo-v2-pro":                  {Context: 1_048_576, MaxOutput: 128_000, Path: "providers/opencode-go/models/mimo-v2-pro.toml"},
	"opencode-go/mimo-v2.5-pro":                {Context: 1_048_576, MaxOutput: 128_000, Path: "providers/opencode-go/models/mimo-v2.5-pro.toml"},
	"opencode-go/mimo-v2.5":                    {Context: 1_000_000, MaxOutput: 128_000, Path: "providers/opencode-go/models/mimo-v2.5.toml"},
	"opencode-go/minimax-m2.5":                 {Context: 204_800, MaxOutput: 65_536, Path: "providers/opencode-go/models/minimax-m2.5.toml"},
	"opencode-go/minimax-m2.7":                 {Context: 204_800, MaxOutput: 131_072, Path: "providers/opencode-go/models/minimax-m2.7.toml"},
	"opencode-go/minimax-m3":                   {Context: 1_000_000, MaxOutput: 131_072, Path: "providers/opencode-go/models/minimax-m3.toml"},
	"opencode-go/muse-spark-1.2-contributor":    {Context: 1_048_576, MaxOutput: 131_072, Path: "models/meta/muse-spark-1.2.toml"},
	"opencode-go/muse-spark-1.3-contributor":    {Context: 1_048_576, MaxOutput: 131_072, Path: "providers/opencode-go/models/muse-spark-1.3-contributor.toml"},
	"opencode-go/omen-alpha":                   {Context: 500_000, MaxOutput: 128_000, Path: "providers/opencode-go/models/omen-alpha.toml"},
	"opencode-go/ox-alpha-free":                {Context: 1_000_000, MaxOutput: 131_072, Path: "providers/opencode-go/models/ox-alpha-free.toml"},
	"opencode-go/qwen3.5-plus":                 {Context: 262_144, MaxOutput: 65_536, Path: "providers/opencode-go/models/qwen3.5-plus.toml"},
	"opencode-go/qwen3.6-plus":                 {Context: 1_000_000, MaxOutput: 65_536, Path: "providers/opencode-go/models/qwen3.6-plus.toml"},
	"opencode-go/qwen3.7-max":                  {Context: 1_000_000, MaxOutput: 65_536, Path: "providers/opencode-go/models/qwen3.7-max.toml"},
	"opencode-go/qwen3.7-plus":                 {Context: 1_000_000, MaxOutput: 65_536, Path: "providers/opencode-go/models/qwen3.7-plus.toml"},
	"opencode-go/qwen3.8-flash":                {Context: 1_000_000, MaxOutput: 131_072, Path: "models/alibaba/qwen3.8-flash.toml"},
	"opencode-go/qwen3.8-max":                  {Context: 1_000_000, MaxOutput: 131_072, Path: "models/alibaba/qwen3.8-max.toml"},
}

func CuratedContextFor(providerID, modelID string) (ContextWindow, int, bool) {
	key := strings.TrimSpace(providerID) + "/" + strings.TrimSpace(modelID)
	row, ok := curatedContextRows[key]
	if !ok || row.Context <= 0 {
		return ContextWindow{}, 0, false
	}
	return ContextWindow{
		Tokens: row.Context,
		Source: ContextCuratedMetadata,
		Evidence: &ContextEvidence{
			Source:   "models.dev",
			Revision: curatedContextMetadataRevision,
			Path:     row.Path,
		},
	}, row.MaxOutput, true
}
