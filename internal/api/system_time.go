package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type systemTimeInfo struct {
	Now           string `json:"now"`
	Timezone      string `json:"timezone"`
	OffsetSeconds int    `json:"offset_seconds"`
	Source        string `json:"source"`
}

type systemTimeProvider interface {
	Snapshot() systemTimeInfo
}

type systemTimeDependencies struct {
	now      func() time.Time
	getenv   func(string) string
	readFile func(string) ([]byte, error)
	readlink func(string) (string, error)
}

type osSystemTimeProvider struct {
	deps systemTimeDependencies
}

func newOSSystemTimeProvider() osSystemTimeProvider {
	return osSystemTimeProvider{deps: systemTimeDependencies{
		now:      time.Now,
		getenv:   os.Getenv,
		readFile: os.ReadFile,
		readlink: os.Readlink,
	}}
}

func (p osSystemTimeProvider) Snapshot() systemTimeInfo {
	now := p.deps.now()
	zoneName, source, location := resolveSystemLocation(now, p.deps)
	if location == nil {
		_, offset := now.Zone()
		location = time.FixedZone("device", offset)
	}
	deviceNow := now.In(location)
	_, offset := deviceNow.Zone()
	return systemTimeInfo{
		Now:           deviceNow.Format(time.RFC3339Nano),
		Timezone:      zoneName,
		OffsetSeconds: offset,
		Source:        source,
	}
}

func resolveSystemLocation(now time.Time, deps systemTimeDependencies) (string, string, *time.Location) {
	candidates := []struct {
		name   string
		source string
	}{
		{name: now.Location().String(), source: "runtime_location"},
		{name: strings.TrimPrefix(strings.TrimSpace(deps.getenv("TZ")), ":"), source: "env_tz"},
		{name: readTimezoneFile(deps.readFile), source: "etc_timezone"},
		{name: readLocaltimeZone(deps.readlink), source: "etc_localtime"},
	}
	for _, candidate := range candidates {
		if location := loadNamedLocation(candidate.name); location != nil {
			return candidate.name, candidate.source, location
		}
	}
	return "", "fixed_offset", nil
}

func loadNamedLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || name == "Local" {
		return nil
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return location
}

func readTimezoneFile(readFile func(string) ([]byte, error)) string {
	content, err := readFile("/etc/timezone")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func readLocaltimeZone(readlink func(string) (string, error)) string {
	target, err := readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join("/etc", target)
	}
	cleaned := filepath.Clean(target)
	marker := string(filepath.Separator) + "zoneinfo" + string(filepath.Separator)
	index := strings.LastIndex(cleaned, marker)
	if index < 0 {
		return ""
	}
	name := strings.TrimPrefix(cleaned[index+len(marker):], "posix/")
	return strings.TrimPrefix(name, "right/")
}

func (s *Server) currentSystemTimeProvider() systemTimeProvider {
	if s.systemTime != nil {
		return s.systemTime
	}
	provider := newOSSystemTimeProvider()
	return provider
}

func (s *Server) handleSystemTime(c *gin.Context) {
	c.JSON(http.StatusOK, s.currentSystemTimeProvider().Snapshot())
}
