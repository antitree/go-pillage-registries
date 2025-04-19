package pillage

import (
	"log"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
)

func LogInfo(format string, args ...interface{}) {
	log.Printf(colorBlue+"[INFO] "+format+colorReset, args...)
}

func LogWarn(format string, args ...interface{}) {
	log.Printf(colorYellow+"[WARN] "+format+colorReset, args...)
}

func LogError(format string, args ...interface{}) {
	log.Printf(colorRed+"[ERROR] "+format+colorReset, args...)
}
