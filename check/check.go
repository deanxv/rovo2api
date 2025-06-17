package check

import (
	"rovo2api/common/config"
	logger "rovo2api/common/loggger"
)

func CheckEnvVariable() {
	logger.SysLog("environment variable checking...")

	if config.RVCookie == "" {
		logger.FatalLog("环境变量 RV_COOKIE 未设置")
	}

	logger.SysLog("environment variable check passed.")
}
