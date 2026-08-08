package logging

import (
	"sync"
	"testing"
)

func TestDetectGoRunModeHonorsTrueOverride(t *testing.T) {
	t.Setenv(forceGoRunEnvironment, " TRUE ")
	if !detectGoRunMode() {
		t.Fatal("true override was ignored")
	}
}

func TestDetectGoRunModeHonorsFalseOverride(t *testing.T) {
	t.Setenv(forceGoRunEnvironment, "false")
	if detectGoRunMode() {
		t.Fatal("false override was ignored")
	}
}

func TestIsGoRunCachesDetection(t *testing.T) {
	t.Cleanup(func() { runModeOnce, goRunMode = sync.Once{}, false })
	t.Setenv(forceGoRunEnvironment, "true")
	runModeOnce = sync.Once{}
	goRunMode = false
	if !IsGoRun() {
		t.Fatal("initial run mode detection = false")
	}
	t.Setenv(forceGoRunEnvironment, "false")
	if !IsGoRun() {
		t.Fatal("cached run mode changed after environment update")
	}
}
