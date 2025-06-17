package common

import "time"

var StartTime = time.Now().Unix() // unit: second
var Version = "v1.0.0"            // this hard coding will be replaced automatically when building, no need to manually change

type ModelInfo struct {
	Model     string
	MaxTokens int
}

// 创建映射表（假设用 model 名称作为 key）
var ModelRegistry = map[string]ModelInfo{
	"anthropic:claude-3-5-sonnet-v2@20241022": {"claude-3.5-sonnet-v2@20241022", 200000},
	"anthropic:claude-3-7-sonnet@20250219":    {"claude-3.7-sonnet@20250219", 200000},
	"anthropic:claude-sonnet-4@20250514":      {"claude-sonnet-4@20250514", 200000},
	"anthropic:claude-opus-4@20250514":        {"claude-opus-4@20250514", 200000},
	//"google:gemini-2.0-flash-001":                       {"gemini-2.0-flash-001", 65535},
	//"google:gemini-2.5-pro-preview-03-25":               {"gemini-2.5-pro-preview-03-25", 65535},
	//"google:gemini-2.5-flash-preview-04-17":             {"gemini-2.5-flash-preview-04-17", 65535},
	"bedrock:anthropic.claude-3-5-sonnet-20241022-v2:0": {"anthropic.claude-3-5-sonnet-20241022-v2:0", 200000},
	"bedrock:anthropic.claude-3-7-sonnet-20250219-v1:0": {"anthropic.claude-3-7-sonnet-20250219-v1:0", 200000},
	"bedrock:anthropic.claude-sonnet-4-20250514-v1:0":   {"anthropic.claude-sonnet-4-20250514-v1:0", 200000},
	"bedrock:anthropic.claude-opus-4-20250514-v1:0":     {"anthropic.claude-opus-4-20250514-v1:0", 200000},
}

// 获取模型信息
func GetModelInfo(modelName string) (ModelInfo, bool) {
	info, ok := ModelRegistry[modelName]
	return info, ok
}

// 获取所有支持的模型列表
func GetModelList() []string {
	var models []string
	for k := range ModelRegistry {
		models = append(models, k)
	}
	return models
}
