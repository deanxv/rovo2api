package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"rovo2api/common"
	"rovo2api/common/config"
	logger "rovo2api/common/loggger"
	"rovo2api/cycletls"
	"rovo2api/model"
	rovoapi "rovo2api/rovo-api"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	errServerErrMsg  = "Service Unavailable"
	responseIDFormat = "chatcmpl-%s"
)

// ChatForOpenAI @Summary OpenAI对话接口
// @Description OpenAI对话接口
// @Tags OpenAI
// @Accept json
// @Produce json
// @Param req body model.OpenAIChatCompletionRequest true "OpenAI对话请求"
// @Param Authorization header string true "Authorization API-KEY"
// @Router /v1/chat/completions [post]
func ChatForOpenAI(c *gin.Context) {
	client := cycletls.Init()
	defer safeClose(client)

	var openAIReq model.OpenAIChatCompletionRequest
	if err := c.BindJSON(&openAIReq); err != nil {
		logger.Errorf(c.Request.Context(), err.Error())
		c.JSON(http.StatusInternalServerError, model.OpenAIErrorResponse{
			OpenAIError: model.OpenAIError{
				Message: "Invalid request parameters",
				Type:    "request_error",
				Code:    "500",
			},
		})
		return
	}

	openAIReq.RemoveEmptyContentMessages()

	modelInfo, b := common.GetModelInfo(openAIReq.Model)
	if !b {
		c.JSON(http.StatusBadRequest, model.OpenAIErrorResponse{
			OpenAIError: model.OpenAIError{
				Message: fmt.Sprintf("Model %s not supported", openAIReq.Model),
				Type:    "invalid_request_error",
				Code:    "invalid_model",
			},
		})
		return
	}
	if openAIReq.MaxTokens > modelInfo.MaxTokens {
		c.JSON(http.StatusBadRequest, model.OpenAIErrorResponse{
			OpenAIError: model.OpenAIError{
				Message: fmt.Sprintf("Max tokens %d exceeds limit %d", openAIReq.MaxTokens, modelInfo.MaxTokens),
				Type:    "invalid_request_error",
				Code:    "invalid_max_tokens",
			},
		})
		return
	}

	if openAIReq.Stream {
		handleStreamRequest(c, client, openAIReq, modelInfo)
	} else {
		handleNonStreamRequest(c, client, openAIReq, modelInfo)
	}
}

func handleNonStreamRequest(c *gin.Context, client cycletls.CycleTLS, openAIReq model.OpenAIChatCompletionRequest, modelInfo common.ModelInfo) {
	ctx := c.Request.Context()
	cookieManager := config.NewCookieManager()

	if config.CustomHeaderKeyEnabled {
		// 从请求头中获取自定义键
		cookie := c.Request.Header.Get("Authorization")
		if cookie == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is required"})
			return
		}
		cookie = strings.Replace(cookie, "Bearer ", "", 1)
		customKeysList := strings.Split(cookie, ",")
		if len(customKeysList) > 0 {
			// 随机选择一个键
			randomIndex := rand.Intn(len(customKeysList))
			selectedKey := strings.TrimSpace(customKeysList[randomIndex])
			cookieManager.Cookies = []string{selectedKey}
		}
	}

	maxRetries := len(cookieManager.Cookies)
	cookie, err := cookieManager.GetRandomCookie()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	for attempt := 0; attempt < maxRetries; attempt++ {
		requestBody, err := createRequestBody(c, &openAIReq, modelInfo)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to marshal request body"})
			return
		}
		sseChan, err := rovoapi.MakeStreamChatRequest(c, client, jsonData, cookie, modelInfo)
		if err != nil {
			logger.Errorf(ctx, "MakeStreamChatRequest err on attempt %d: %v", attempt+1, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		isRateLimit := false
		var delta string
		var assistantMsgContent string
		var shouldContinue bool
		thinkStartType := new(bool)
		thinkEndType := new(bool)
	SSELoop:
		for response := range sseChan {
			data := response.Data
			if data == "" {
				continue
			}
			if response.Done {
				switch {
				case common.IsUsageLimitExceeded(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie Usage limit exceeded, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					config.RemoveCookie(cookie)
					break SSELoop
				case common.IsServerError(data):
					logger.Errorf(ctx, errServerErrMsg)
					c.JSON(http.StatusInternalServerError, gin.H{"error": errServerErrMsg})
					return
				case common.IsNotLogin(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie Not Login, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					break SSELoop
				case common.IsRateLimit(data):
					isRateLimit = true
					logger.Warnf(ctx, "Cookie rate limited, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
					config.AddRateLimitCookie(cookie, time.Now().Add(time.Duration(config.RateLimitCookieLockDuration)*time.Second))
					break SSELoop
				}
				logger.Warnf(ctx, response.Data)
				return
			}

			logger.Debug(ctx, strings.TrimSpace(data))

			streamDelta, streamShouldContinue := processNoStreamData(c, data, modelInfo, thinkStartType, thinkEndType)
			delta = streamDelta
			shouldContinue = streamShouldContinue
			// 处理事件流数据
			if !shouldContinue {
				promptTokens := model.CountTokenText(string(jsonData), openAIReq.Model)
				completionTokens := model.CountTokenText(assistantMsgContent, openAIReq.Model)
				finishReason := "stop"

				c.JSON(http.StatusOK, model.OpenAIChatCompletionResponse{
					ID:      fmt.Sprintf(responseIDFormat, time.Now().Format("20060102150405")),
					Object:  "chat.completion",
					Created: time.Now().Unix(),
					Model:   openAIReq.Model,
					Choices: []model.OpenAIChoice{{
						Message: model.OpenAIMessage{
							Role:    "assistant",
							Content: assistantMsgContent,
						},
						FinishReason: &finishReason,
					}},
					Usage: model.OpenAIUsage{
						PromptTokens:     promptTokens,
						CompletionTokens: completionTokens,
						TotalTokens:      promptTokens + completionTokens,
					},
				})

				return
			} else {
				assistantMsgContent = assistantMsgContent + delta
			}
		}
		if !isRateLimit {
			return
		}

		// 获取下一个可用的cookie继续尝试
		cookie, err = cookieManager.GetNextCookie()
		if err != nil {
			logger.Errorf(ctx, "No more valid cookies available after attempt %d", attempt+1)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

	}
	logger.Errorf(ctx, "All cookies exhausted after %d attempts", maxRetries)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "All cookies are temporarily unavailable."})
	return
}

func createRequestBody(c *gin.Context, openAIReq *model.OpenAIChatCompletionRequest, modelInfo common.ModelInfo) (map[string]interface{}, error) {
	// 创建请求体
	logger.Debug(c.Request.Context(), fmt.Sprintf("RequestBody: %v", openAIReq))

	if config.PRE_MESSAGES_JSON != "" {
		err := openAIReq.PrependMessagesFromJSON(config.PRE_MESSAGES_JSON)
		if err != nil {
			return nil, fmt.Errorf("PrependMessagesFromJSON err: %v JSON:%s", err, config.PRE_MESSAGES_JSON)
		}
	}

	if openAIReq.MaxTokens <= 1 {
		openAIReq.MaxTokens = 8192
	}

	// 将消息格式化为Atlassian API接受的格式
	formattedMessages := transformMessages(openAIReq.Messages)

	// 创建最终请求
	upstreamRequest := map[string]interface{}{
		"request_payload": map[string]interface{}{
			"messages":          formattedMessages,
			"stream":            "true",
			"temperature":       openAIReq.Temperature,
			"max_tokens":        openAIReq.MaxTokens,
			"frequency_penalty": openAIReq.FrequencyPenalty,
			"presence_penalty":  openAIReq.PresencePenalty,
			"top_p":             openAIReq.TopP,
		},
		"platform_attributes": map[string]interface{}{
			"model": transformModelId(openAIReq.Model),
		},
	}

	return upstreamRequest, nil
}

// 转换模型ID，从前缀:模型格式提取出真实的模型ID
func transformModelId(modelId string) string {
	parts := strings.Split(modelId, ":")
	if len(parts) > 1 {
		return strings.Join(parts[1:], ":")
	}
	return modelId
}

// 将OpenAI消息格式转换为Atlassian API接受的格式
func transformMessages(messages []model.OpenAIChatMessage) []map[string]interface{} {
	var result []map[string]interface{}

	for _, msg := range messages {
		var contentItems []map[string]interface{}

		switch content := msg.Content.(type) {
		case string:
			// 如果是字符串，转换为数组格式
			contentItems = []map[string]interface{}{
				{
					"type": "text",
					"text": content,
				},
			}
		case []interface{}:
			// 如果已经是数组格式，处理每个项目
			for _, item := range content {
				if itemMap, ok := item.(map[string]interface{}); ok {
					itemType, _ := itemMap["type"].(string)
					if itemType == "text" {
						text, _ := itemMap["text"].(string)
						contentItems = append(contentItems, map[string]interface{}{
							"type": "text",
							"text": text,
						})
					} else if itemType == "image_url" {
						// 处理图像URL
						if imageData, ok := itemMap["image_url"].(map[string]interface{}); ok {
							url, _ := imageData["url"].(string)
							contentItems = append(contentItems, map[string]interface{}{
								"type": "image",
								"image": map[string]interface{}{
									"url": url,
								},
							})
						}
					}
				}
			}
		default:
			// 其他情况转换为文本格式
			contentStr := fmt.Sprintf("%v", msg.Content)
			contentItems = []map[string]interface{}{
				{
					"type": "text",
					"text": contentStr,
				},
			}
		}

		message := map[string]interface{}{
			"role":    msg.Role,
			"content": contentItems,
		}
		result = append(result, message)
	}

	return result
}

// createStreamResponse 创建流式响应
func createStreamResponse(responseId, modelName string, jsonData []byte, delta model.OpenAIDelta, finishReason *string) model.OpenAIChatCompletionResponse {
	promptTokens := model.CountTokenText(string(jsonData), modelName)
	completionTokens := model.CountTokenText(delta.Content, modelName)
	return model.OpenAIChatCompletionResponse{
		ID:      responseId,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   modelName,
		Choices: []model.OpenAIChoice{
			{
				Index:        0,
				Delta:        delta,
				FinishReason: finishReason,
			},
		},
		Usage: model.OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// handleDelta 处理消息字段增量
func handleDelta(c *gin.Context, delta string, responseId, modelName string, jsonData []byte) error {
	// 创建基础响应
	createResponse := func(content string) model.OpenAIChatCompletionResponse {
		return createStreamResponse(
			responseId,
			modelName,
			jsonData,
			model.OpenAIDelta{Content: content, Role: "assistant"},
			nil,
		)
	}

	// 发送基础事件
	var err error
	if err = sendSSEvent(c, createResponse(delta)); err != nil {
		return err
	}

	return err
}

// handleMessageResult 处理消息结果
func handleMessageResult(c *gin.Context, responseId, modelName string, jsonData []byte) bool {
	finishReason := "stop"
	var delta string

	promptTokens := 0
	completionTokens := 0

	streamResp := createStreamResponse(responseId, modelName, jsonData, model.OpenAIDelta{Content: delta, Role: "assistant"}, &finishReason)
	streamResp.Usage = model.OpenAIUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}

	if err := sendSSEvent(c, streamResp); err != nil {
		logger.Warnf(c.Request.Context(), "sendSSEvent err: %v", err)
		return false
	}
	c.SSEvent("", " [DONE]")
	return false
}

// sendSSEvent 发送SSE事件
func sendSSEvent(c *gin.Context, response model.OpenAIChatCompletionResponse) error {
	jsonResp, err := json.Marshal(response)
	if err != nil {
		logger.Errorf(c.Request.Context(), "Failed to marshal response: %v", err)
		return err
	}
	c.SSEvent("", " "+string(jsonResp))
	c.Writer.Flush()
	return nil
}

func handleStreamRequest(c *gin.Context, client cycletls.CycleTLS, openAIReq model.OpenAIChatCompletionRequest, modelInfo common.ModelInfo) {

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	responseId := fmt.Sprintf(responseIDFormat, time.Now().Format("20060102150405"))
	ctx := c.Request.Context()

	cookieManager := config.NewCookieManager()

	if config.CustomHeaderKeyEnabled {
		// 从请求头中获取自定义键
		cookie := c.Request.Header.Get("Authorization")
		if cookie == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header is required"})
			return
		}
		cookie = strings.Replace(cookie, "Bearer ", "", 1)
		customKeysList := strings.Split(cookie, ",")
		if len(customKeysList) > 0 {
			// 随机选择一个键
			randomIndex := rand.Intn(len(customKeysList))
			selectedKey := strings.TrimSpace(customKeysList[randomIndex])
			cookieManager.Cookies = []string{selectedKey}
		}
	}

	maxRetries := len(cookieManager.Cookies)
	cookie, err := cookieManager.GetRandomCookie()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	thinkStartType := new(bool)
	thinkEndType := new(bool)

	c.Stream(func(w io.Writer) bool {
		for attempt := 0; attempt < maxRetries; attempt++ {
			requestBody, err := createRequestBody(c, &openAIReq, modelInfo)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return false
			}

			jsonData, err := json.Marshal(requestBody)
			if err != nil {
				c.JSON(500, gin.H{"error": "Failed to marshal request body"})
				return false
			}
			sseChan, err := rovoapi.MakeStreamChatRequest(c, client, jsonData, cookie, modelInfo)
			if err != nil {
				logger.Errorf(ctx, "MakeStreamChatRequest err on attempt %d: %v", attempt+1, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return false
			}

			isRateLimit := false
		SSELoop:
			for response := range sseChan {

				if response.Status == 403 {
					c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
					config.RemoveCookie(cookie)
					isRateLimit = true
					break SSELoop
				}
				if response.Status == 401 {
					//c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized"})
					config.RemoveCookie(cookie)
					isRateLimit = true
					break SSELoop
				}

				data := response.Data
				if data == "" {
					continue
				}

				if response.Done {
					switch {
					case common.IsUsageLimitExceeded(data):
						isRateLimit = true
						logger.Warnf(ctx, "Cookie Usage limit exceeded, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
						config.RemoveCookie(cookie)
						break SSELoop
					case common.IsServerError(data):
						logger.Errorf(ctx, errServerErrMsg)
						c.JSON(http.StatusInternalServerError, gin.H{"error": errServerErrMsg})
						return false
					case common.IsNotLogin(data):
						isRateLimit = true
						logger.Warnf(ctx, "Cookie Not Login, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
						break SSELoop // 使用 label 跳出 SSE 循环
					case common.IsRateLimit(data):
						isRateLimit = true
						logger.Warnf(ctx, "Cookie rate limited, switching to next cookie, attempt %d/%d, COOKIE:%s", attempt+1, maxRetries, cookie)
						config.AddRateLimitCookie(cookie, time.Now().Add(time.Duration(config.RateLimitCookieLockDuration)*time.Second))
						break SSELoop
					}
					logger.Warnf(ctx, response.Data)
					return false
				}

				logger.Debug(ctx, strings.TrimSpace(data))

				_, shouldContinue := processStreamData(c, data, responseId, openAIReq.Model, modelInfo, jsonData, thinkStartType, thinkEndType)
				// 处理事件流数据

				if !shouldContinue {
					return false
				}
			}

			if !isRateLimit {
				return true
			}

			// 获取下一个可用的cookie继续尝试
			cookie, err = cookieManager.GetNextCookie()
			if err != nil {
				logger.Errorf(ctx, "No more valid cookies available after attempt %d", attempt+1)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return false
			}
		}

		logger.Errorf(ctx, "All cookies exhausted after %d attempts", maxRetries)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "All cookies are temporarily unavailable."})
		return false
	})
}

// 处理流式数据的辅助函数，返回bool表示是否继续处理
func processStreamData(c *gin.Context, data, responseId, model string, modelInfo common.ModelInfo, jsonData []byte, thinkStartType, thinkEndType *bool) (string, bool) {
	data = strings.TrimSpace(data)
	data = strings.TrimPrefix(data, "data: ")

	// 处理[DONE]标记
	if data == "[DONE]" {
		return "", false
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		logger.Errorf(c.Request.Context(), "Failed to unmarshal event: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return "", false
	}

	// 获取response_payload
	responsePayload, ok := event["response_payload"].(map[string]interface{})
	if !ok {
		logger.Errorf(c.Request.Context(), "Invalid response format: response_payload not found")
		return "", false
	}

	// 检查是否有choices数组
	choices, ok := responsePayload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		logger.Errorf(c.Request.Context(), "Invalid response format: choices not found or empty")
		return "", false
	}

	// 获取第一个choice
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		logger.Errorf(c.Request.Context(), "Invalid choice format in response")
		return "", false
	}

	// 检查是否完成
	finishReason, hasFinishReason := choice["finish_reason"]
	if hasFinishReason && finishReason != nil && finishReason.(string) == "end_turn" {
		// 处理完成的消息
		handleMessageResult(c, responseId, model, jsonData)
		return "", false // 标记为结束
	}

	// 获取message内容
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		logger.Errorf(c.Request.Context(), "Message not found in response")
		return "", true
	}

	// 获取content数组
	contentArray, ok := message["content"].([]interface{})
	if !ok {
		// 可能是空数组或其他格式，继续处理
		return "", true
	}

	// 如果content数组为空，继续处理
	if len(contentArray) == 0 {
		return "", true
	}

	// 处理content数组中的每个元素
	for _, item := range contentArray {
		contentItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// 检查类型是否为text
		contentType, ok := contentItem["type"].(string)
		if !ok || contentType != "text" {
			continue
		}

		// 获取文本内容
		text, ok := contentItem["text"].(string)
		if !ok {
			continue
		}

		// 处理文本内容
		if err := handleDelta(c, text, responseId, model, jsonData); err != nil {
			logger.Errorf(c.Request.Context(), "handleDelta err: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return "", false
		}

		return text, true
	}

	return "", true
}

func processNoStreamData(c *gin.Context, data string, modelInfo common.ModelInfo, thinkStartType *bool, thinkEndType *bool) (string, bool) {
	data = strings.TrimSpace(data)
	data = strings.TrimPrefix(data, "data: ")

	// 处理[DONE]标记
	if data == "[DONE]" {
		return "", false
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		logger.Errorf(c.Request.Context(), "Failed to unmarshal event: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return "", false
	}

	// 获取response_payload
	responsePayload, ok := event["response_payload"].(map[string]interface{})
	if !ok {
		logger.Errorf(c.Request.Context(), "Invalid response format: response_payload not found")
		return "", false
	}

	// 检查是否有choices数组
	choices, ok := responsePayload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		logger.Errorf(c.Request.Context(), "Invalid response format: choices not found or empty")
		return "", false
	}

	// 获取第一个choice
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		logger.Errorf(c.Request.Context(), "Invalid choice format in response")
		return "", false
	}

	// 检查是否完成
	finishReason, hasFinishReason := choice["finish_reason"]
	if hasFinishReason && finishReason != nil && finishReason.(string) == "end_turn" {
		// 处理完成的消息
		return "", false // 标记为结束
	}

	// 获取message内容
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		logger.Errorf(c.Request.Context(), "Message not found in response")
		return "", true
	}

	// 获取content数组
	contentArray, ok := message["content"].([]interface{})
	if !ok {
		// 可能是空数组或其他格式，继续处理
		return "", true
	}

	// 如果content数组为空，继续处理
	if len(contentArray) == 0 {
		return "", true
	}

	// 收集所有文本内容
	var contentText string
	for _, item := range contentArray {
		contentItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// 检查类型是否为text
		contentType, ok := contentItem["type"].(string)
		if !ok || contentType != "text" {
			continue
		}

		// 获取文本内容
		text, ok := contentItem["text"].(string)
		if !ok {
			continue
		}

		contentText += text
	}

	return contentText, true
}

// OpenaiModels @Summary OpenAI模型列表接口
// @Description OpenAI模型列表接口
// @Tags OpenAI
// @Accept json
// @Produce json
// @Param Authorization header string true "Authorization API-KEY"
// @Success 200 {object} common.ResponseResult{data=model.OpenaiModelListResponse} "成功"
// @Router /v1/models [get]
func OpenaiModels(c *gin.Context) {
	var modelsResp []string

	modelsResp = common.GetModelList()

	var openaiModelListResponse model.OpenaiModelListResponse
	var openaiModelResponse []model.OpenaiModelResponse
	openaiModelListResponse.Object = "list"

	for _, modelResp := range modelsResp {
		openaiModelResponse = append(openaiModelResponse, model.OpenaiModelResponse{
			ID:     modelResp,
			Object: "model",
		})
	}
	openaiModelListResponse.Data = openaiModelResponse
	c.JSON(http.StatusOK, openaiModelListResponse)
	return
}

func safeClose(client cycletls.CycleTLS) {
	if client.ReqChan != nil {
		close(client.ReqChan)
	}
	if client.RespChan != nil {
		close(client.RespChan)
	}
}

//
//func processUrl(c *gin.Context, client cycletls.CycleTLS, chatId, cookie string, url string) (string, error) {
//	// 判断是否为URL
//	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
//		// 下载文件
//		bytes, err := fetchImageBytes(url)
//		if err != nil {
//			logger.Errorf(c.Request.Context(), fmt.Sprintf("fetchImageBytes err  %v\n", err))
//			return "", fmt.Errorf("fetchImageBytes err  %v\n", err)
//		}
//
//		base64Str := base64.StdEncoding.EncodeToString(bytes)
//
//		finalUrl, err := processBytes(c, client, chatId, cookie, base64Str)
//		if err != nil {
//			logger.Errorf(c.Request.Context(), fmt.Sprintf("processBytes err  %v\n", err))
//			return "", fmt.Errorf("processBytes err  %v\n", err)
//		}
//		return finalUrl, nil
//	} else {
//		finalUrl, err := processBytes(c, client, chatId, cookie, url)
//		if err != nil {
//			logger.Errorf(c.Request.Context(), fmt.Sprintf("processBytes err  %v\n", err))
//			return "", fmt.Errorf("processBytes err  %v\n", err)
//		}
//		return finalUrl, nil
//	}
//}
//
//func fetchImageBytes(url string) ([]byte, error) {
//	resp, err := http.Get(url)
//	if err != nil {
//		return nil, fmt.Errorf("http.Get err: %v\n", err)
//	}
//	defer resp.Body.Close()
//
//	return io.ReadAll(resp.Body)
//}
//
//func processBytes(c *gin.Context, client cycletls.CycleTLS, chatId, cookie string, base64Str string) (string, error) {
//	// 检查类型
//	fileType := common.DetectFileType(base64Str)
//	if !fileType.IsValid {
//		return "", fmt.Errorf("invalid file type %s", fileType.Extension)
//	}
//	signUrl, err := rovo_api.GetSignURL(client, cookie, chatId, fileType.Extension)
//	if err != nil {
//		logger.Errorf(c.Request.Context(), fmt.Sprintf("GetSignURL err  %v\n", err))
//		return "", fmt.Errorf("GetSignURL err: %v\n", err)
//	}
//
//	err = rovo_api.UploadToS3(client, signUrl, base64Str, fileType.MimeType)
//	if err != nil {
//		logger.Errorf(c.Request.Context(), fmt.Sprintf("UploadToS3 err  %v\n", err))
//		return "", err
//	}
//
//	u, err := url.Parse(signUrl)
//	if err != nil {
//		return "", err
//	}
//
//	return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path), nil
//}

func checkStatusEquals(status int, expected int) bool {
	return status == expected
}
