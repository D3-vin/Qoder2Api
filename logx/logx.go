// Package logx provides a tiny env-configurable logger.
// Level is read once from QODER_LOG_LEVEL: debug | info (default) | warn | error.
package logx

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

const (
	lDebug = 0
	lInfo  = 1
	lWarn  = 2
	lError = 3
)

var (
	once  sync.Once
	level = lInfo
)

func current() int {
	once.Do(func() {
		switch strings.ToLower(strings.TrimSpace(os.Getenv("QODER_LOG_LEVEL"))) {
		case "debug":
			level = lDebug
		case "warn", "warning":
			level = lWarn
		case "error":
			level = lError
		default:
			level = lInfo
		}
	})
	return level
}

// Debugf prints verbose diagnostics only when QODER_LOG_LEVEL=debug.
func Debugf(format string, args ...interface{}) {
	if current() <= lDebug {
		fmt.Printf(format, args...)
	}
}

// Infof prints operational messages at the default level.
func Infof(format string, args ...interface{}) {
	if current() <= lInfo {
		fmt.Printf(format, args...)
	}
}

// Warnf prints warnings at warn level and above.
func Warnf(format string, args ...interface{}) {
	if current() <= lWarn {
		fmt.Printf(format, args...)
	}
}
