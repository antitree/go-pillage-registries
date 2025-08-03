package pillage

import (
	"log"
)

var debugEnabled bool

// SetDebug enables or disables debug logging.
func SetDebug(enabled bool) {
	debugEnabled = enabled
}

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
)

// LogInfo logs informational messages prefixed with [INFO].
func LogInfo(format string, args ...interface{}) {
	log.Printf(colorBlue+"[INFO] "+format+colorReset, args...)
}

// LogWarn logs warning messages prefixed with [WARN].
func LogWarn(format string, args ...interface{}) {
	log.Printf(colorYellow+"[WARN] "+format+colorReset, args...)
}

// LogDebug logs debug messages prefixed with [DEBUG] when debug is enabled.
func LogDebug(format string, args ...interface{}) {
	if !debugEnabled {
		return
	}
	log.Printf(colorGray+"[DEBUG] "+format+colorReset, args...)
}

// LogError logs error messages prefixed with [ERROR].
func LogError(format string, args ...interface{}) {
	log.Printf(colorRed+"[ERROR] "+format+colorReset, args...)
}
