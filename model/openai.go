package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

type OpenAIChatCompletionRequest struct {
	Model            string              `json:"model"`
	Stream           bool                `json:"stream"`
	Messages         []OpenAIChatMessage `json:"messages"`
	MaxTokens        int                 `json:"max_tokens"`
	Temperature      float64             `json:"temperature"`
	FrequencyPenalty float64             `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64             `json:"presence_penalty,omitempty"`
	TopP             float64             `json:"top_p,omitempty"`
}

type OpenAIChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// 修正后的Claude请求结构
type ClaudeCompletionRequest struct {
	Model       string                `json:"model"`
	MaxTokens   int                   `json:"max_tokens"`
	Temperature float64               `json:"temperature"`
	System      []ClaudeSystemMessage `json:"system,omitempty"`
	Messages    []ClaudeMessage       `json:"messages,omitempty"`
	Stream      bool                  `json:"stream,omitempty"`
	Thinking    *ClaudeThinking       `json:"thinking,omitempty"`
}

// 单独定义 Thinking 结构体
type ClaudeThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// 修正后的Claude系统消息结构，添加了Type字段
type ClaudeSystemMessage struct {
	Type         string `json:"type"` // 添加type字段
	Text         string `json:"text"`
	CacheControl struct {
		Type string `json:"type"`
	} `json:"cache_control"`
}

type ClaudeMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// GeminiCompletionRequest 定义Gemini请求结构
type GeminiCompletionRequest struct {
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   float64         `json:"temperature"`
	System        string          `json:"system,omitempty"` // 顶层system参数
	Messages      []GeminiMessage `json:"messages"`
	Stream        bool            `json:"stream"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
	Transforms []string `json:"transforms"`
	Reasoning  struct {
		Exclude bool `json:"exclude"`
	} `json:"reasoning"`
}

// GeminiMessage 定义Gemini消息结构
type GeminiMessage struct {
	Role    string          `json:"role"`
	Content []GeminiContent `json:"content"`
}

// GeminiContent 定义Gemini内容结构
type GeminiContent struct {
	Type  string       `json:"type"`
	Text  string       `json:"text,omitempty"`
	Image *GeminiImage `json:"image,omitempty"`
}

// GeminiImage 定义Gemini图像结构
type GeminiImage struct {
	URL string `json:"url"`
}

func processMessageContent(content interface{}) (interface{}, error) {
	// 如果是字符串，直接返回
	if textContent, ok := content.(string); ok {
		return textContent, nil
	}

	// 如果是数组（OpenAI的多模态格式）
	if contentArray, ok := content.([]interface{}); ok {
		var claudeContent []interface{}

		for _, item := range contentArray {
			// 检查每个项目
			if itemMap, ok := item.(map[string]interface{}); ok {
				// 检查类型
				if itemType, ok := itemMap["type"].(string); ok {
					if itemType == "text" {
						// 文本项，直接添加
						if text, ok := itemMap["text"].(string); ok {
							claudeContent = append(claudeContent, map[string]interface{}{
								"type": "text",
								"text": text,
							})
						}
					} else if itemType == "image_url" {
						// 图像URL项，转换格式
						if imageUrl, ok := itemMap["image_url"].(map[string]interface{}); ok {
							if url, ok := imageUrl["url"].(string); ok {
								// 检查是否是base64格式的图像
								if strings.HasPrefix(url, "data:image/") {
									// 提取图像类型和base64数据
									parts := strings.Split(url, ",")
									if len(parts) == 2 {
										mediaTypePart := strings.Split(parts[0], ";")
										if len(mediaTypePart) >= 1 {
											mediaType := strings.TrimPrefix(mediaTypePart[0], "data:")

											// 创建Claude格式的图像
											claudeContent = append(claudeContent, map[string]interface{}{
												"type": "image",
												"source": map[string]interface{}{
													"type":       "base64",
													"media_type": mediaType,
													"data":       parts[1],
												},
											})
										}
									}
								} else {
									// 如果是URL而不是base64，保持原样
									claudeContent = append(claudeContent, map[string]interface{}{
										"type": "image",
										"source": map[string]interface{}{
											"type": "url",
											"url":  url,
										},
									})
								}
							}
						}
					}
				}
			} else if textItem, ok := item.(string); ok {
				// 直接文本项
				claudeContent = append(claudeContent, map[string]interface{}{
					"type": "text",
					"text": textItem,
				})
			}
		}

		return claudeContent, nil
	}

	// 如果是单个对象（可能是单个图像对象）
	if contentMap, ok := content.(map[string]interface{}); ok {
		if contentType, ok := contentMap["type"].(string); ok {
			if contentType == "image" {
				// 这是OpenAI的图像格式，直接返回，因为Claude的格式相似
				return []interface{}{contentMap}, nil
			} else if contentType == "image_url" {
				// 处理OpenAI的image_url格式
				if imageUrl, ok := contentMap["image_url"].(map[string]interface{}); ok {
					if url, ok := imageUrl["url"].(string); ok {
						// 检查是否是base64格式的图像
						if strings.HasPrefix(url, "data:image/") {
							// 提取图像类型和base64数据
							parts := strings.Split(url, ",")
							if len(parts) == 2 {
								mediaTypePart := strings.Split(parts[0], ";")
								if len(mediaTypePart) >= 1 {
									mediaType := strings.TrimPrefix(mediaTypePart[0], "data:")

									// 创建Claude格式的图像
									return []interface{}{
										map[string]interface{}{
											"type": "image",
											"source": map[string]interface{}{
												"type":       "base64",
												"media_type": mediaType,
												"data":       parts[1],
											},
										},
									}, nil
								}
							}
						} else {
							// 如果是URL而不是base64
							return []interface{}{
								map[string]interface{}{
									"type": "image",
									"source": map[string]interface{}{
										"type": "url",
										"url":  url,
									},
								},
							}, nil
						}
					}
				}
			}
		}
	}

	// 无法识别的格式，尝试将其序列化为文本
	contentBytes, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("无法序列化消息内容: %v", err)
	}
	return string(contentBytes), nil
}

func (r *OpenAIChatCompletionRequest) AddMessage(message OpenAIChatMessage) {
	r.Messages = append([]OpenAIChatMessage{message}, r.Messages...)
}

func (r *OpenAIChatCompletionRequest) PrependMessagesFromJSON(jsonString string) error {
	var newMessages []OpenAIChatMessage
	err := json.Unmarshal([]byte(jsonString), &newMessages)
	if err != nil {
		return err
	}

	// 查找最后一个 system role 的索引
	var insertIndex int
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "system" {
			insertIndex = i + 1
			break
		}
	}

	// 将 newMessages 插入到找到的索引后面
	r.Messages = append(r.Messages[:insertIndex], append(newMessages, r.Messages[insertIndex:]...)...)
	return nil
}

func (r *OpenAIChatCompletionRequest) SystemMessagesProcess(model string) {
	if r.Messages == nil {
		return
	}

	for i := range r.Messages {
		if r.Messages[i].Role == "system" {
			r.Messages[i].Role = "user"
		}

	}

}

func (r *OpenAIChatCompletionRequest) FilterUserMessage() {
	if r.Messages == nil {
		return
	}

	// 返回最后一个role为user的元素
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			r.Messages = r.Messages[i:]
			break
		}
	}
}

type OpenAIErrorResponse struct {
	OpenAIError OpenAIError `json:"error"`
}

type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

type OpenAIChatCompletionResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []OpenAIChoice `json:"choices"`
	Usage             OpenAIUsage    `json:"usage"`
	SystemFingerprint *string        `json:"system_fingerprint"`
	Suggestions       []string       `json:"suggestions"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	LogProbs     *string       `json:"logprobs"`
	FinishReason *string       `json:"finish_reason"`
	Delta        OpenAIDelta   `json:"delta"`
}

type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIDelta struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type OpenAIImagesGenerationRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	ResponseFormat string `json:"response_format"`
	Image          string `json:"image"`
}

type OpenAIImagesGenerationResponse struct {
	Created     int64                                 `json:"created"`
	DailyLimit  bool                                  `json:"dailyLimit"`
	Data        []*OpenAIImagesGenerationDataResponse `json:"data"`
	Suggestions []string                              `json:"suggestions"`
}

type OpenAIImagesGenerationDataResponse struct {
	URL           string `json:"url"`
	RevisedPrompt string `json:"revised_prompt"`
	B64Json       string `json:"b64_json"`
}

type OpenAIGPT4VImagesReq struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

type GetUserContent interface {
	GetUserContent() []string
}

type OpenAIModerationRequest struct {
	Input string `json:"input"`
}

type OpenAIModerationResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Results []struct {
		Flagged        bool               `json:"flagged"`
		Categories     map[string]bool    `json:"categories"`
		CategoryScores map[string]float64 `json:"category_scores"`
	} `json:"results"`
}

type OpenaiModelResponse struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	//Created time.Time `json:"created"`
	//OwnedBy string    `json:"owned_by"`
}

// ModelList represents a list of models.
type OpenaiModelListResponse struct {
	Object string                `json:"object"`
	Data   []OpenaiModelResponse `json:"data"`
}

func (r *OpenAIChatCompletionRequest) GetUserContent() []string {
	var userContent []string

	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			switch contentObj := r.Messages[i].Content.(type) {
			case string:
				userContent = append(userContent, contentObj)
			}
			break
		}
	}

	return userContent
}
func (r *OpenAIChatCompletionRequest) GetPreviousMessagePair() (string, bool, error) {
	messages := r.Messages
	if len(messages) < 3 {
		return "", false, nil
	}

	if len(messages) > 0 && messages[len(messages)-1].Role != "user" {
		return "", false, nil
	}

	for i := len(messages) - 2; i > 0; i-- {
		if messages[i].Role == "assistant" {
			if messages[i-1].Role == "user" {
				// 深拷贝消息对象避免污染原始数据
				prevPair := []OpenAIChatMessage{
					messages[i-1], // 用户消息
					messages[i],   // 助手消息
				}

				jsonData, err := json.Marshal(prevPair)
				if err != nil {
					return "", false, err
				}

				// 移除JSON字符串中的转义字符
				cleaned := strings.NewReplacer(
					`\n`, "",
					`\t`, "",
					`\r`, "",
				).Replace(string(jsonData))

				return cleaned, true, nil
			}
		}
	}
	return "", false, nil
}

func (r *OpenAIChatCompletionRequest) RemoveEmptyContentMessages() *OpenAIChatCompletionRequest {
	if r == nil || len(r.Messages) == 0 {
		return r
	}

	var filteredMessages []OpenAIChatMessage
	for _, msg := range r.Messages {
		// Check if content is nil
		if msg.Content == nil {
			continue
		}

		// Check if content is an empty string
		if strContent, ok := msg.Content.(string); ok && strContent == "" {
			continue
		}

		// Check if content is an empty slice
		if sliceContent, ok := msg.Content.([]interface{}); ok && len(sliceContent) == 0 {
			continue
		}

		// If we get here, the content is not empty
		filteredMessages = append(filteredMessages, msg)
	}

	r.Messages = filteredMessages
	return r
}
