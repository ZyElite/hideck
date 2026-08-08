package logging

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

const (
	forceGoRunEnvironment = "VOHIVE_FORCE_GO_RUN_LOG"
	goBuildPathFragment   = "/go-build"
	goRunBuildPath        = "command-line-arguments"
)

var (
	runModeOnce sync.Once
	goRunMode   bool
)

// IsGoRun reports whether the process is executing a binary built by go run.
func IsGoRun() bool {
	runModeOnce.Do(func() {
		goRunMode = detectGoRunMode()
	})
	return goRunMode
}

func detectGoRunMode() bool {
	forced := strings.TrimSpace(os.Getenv(forceGoRunEnvironment))
	if forced != "" {
		if enabled, err := strconv.ParseBool(forced); err == nil {
			return enabled
		}
	}
	if executable, err := os.Executable(); err == nil {
		executable = strings.ToLower(strings.TrimSpace(executable))
		if strings.Contains(executable, goBuildPathFragment) {
			return true
		}
	}
	if buildInfo, ok := debug.ReadBuildInfo(); ok && buildInfo != nil {
		return strings.TrimSpace(buildInfo.Main.Path) == goRunBuildPath
	}
	return false
}
