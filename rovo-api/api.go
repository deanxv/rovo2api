package rovo_api

import (
	"encoding/base64"
	"fmt"
	"github.com/gin-gonic/gin"
	"rovo2api/common"
	"rovo2api/common/config"
	logger "rovo2api/common/loggger"
	"rovo2api/cycletls"
)

const (
	atlassianAPIEndpoint = "https://api.atlassian.com/rovodev/v2/proxy/ai"
	unifiedChatPath      = "/v2/beta/chat"
)

func MakeStreamChatRequest(c *gin.Context, client cycletls.CycleTLS, jsonData []byte, cookie string, modelInfo common.ModelInfo) (<-chan cycletls.SSEResponse, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(cookie))

	endpoint := atlassianAPIEndpoint + unifiedChatPath
	headers := map[string]string{
		"Content-Type":             "application/json",
		"Accept":                   "application/json",
		"Authorization":            "Basic " + encoded,
		"X-Atlassian-EncodedToken": encoded,
	}

	options := cycletls.Options{
		Timeout: 10 * 60 * 60,
		Proxy:   config.ProxyUrl, // 在每个请求中设置代理
		Body:    string(jsonData),
		Method:  "POST",
		Headers: headers,
	}

	logger.Debug(c.Request.Context(), fmt.Sprintf("cookie: %v", cookie))

	sseChan, err := client.DoSSE(endpoint, options, "POST")
	if err != nil {
		logger.Errorf(c, "Failed to make stream request: %v", err)
		return nil, fmt.Errorf("Failed to make stream request: %v", err)
	}
	return sseChan, nil
}
