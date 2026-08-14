package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	dockerHubTagsURL = "https://hub.docker.com/v2/repositories/yibaiba/hideck/tags?page_size=100&ordering=last_updated"
	maxResponseBytes = 2 << 20
)

var (
	ErrDisabled          = errors.New("当前构建不支持应用内更新，请按部署方式重新部署")
	stableVersionPattern = regexp.MustCompile(`^[vV]?(\d+)\.(\d+)(?:\.(\d+))?$`)
)

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

type CheckerOptions struct {
	TagsURL        string
	CurrentVersion string
	IsDocker       bool
}

type Checker struct {
	client         HTTPClient
	tagsURL        string
	currentVersion string
	isDocker       bool
}

type UpdateInfo struct {
	HasUpdate   bool   `json:"has_update"`
	CurrentVer  string `json:"current_version"`
	LatestVer   string `json:"latest_version"`
	ReleaseNote string `json:"release_note"`
	IsDocker    bool   `json:"is_docker"`
}

type dockerHubTagsPage struct {
	Next    string `json:"next"`
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
}

type semanticVersion struct {
	major      uint64
	minor      uint64
	patch      uint64
	components int
	tag        string
}

func NewChecker(client HTTPClient, options CheckerOptions) *Checker {
	tagsURL := strings.TrimSpace(options.TagsURL)
	if tagsURL == "" {
		tagsURL = dockerHubTagsURL
	}
	return &Checker{
		client:         client,
		tagsURL:        tagsURL,
		currentVersion: strings.TrimSpace(options.CurrentVersion),
		isDocker:       options.IsDocker,
	}
}

func (c *Checker) CheckUpdate(ctx context.Context) (*UpdateInfo, error) {
	current, err := parseStableVersion(c.currentVersion)
	if err != nil {
		return nil, fmt.Errorf("当前构建版本无效 %q: %w", c.currentVersion, err)
	}
	latest, err := c.fetchLatestVersion(ctx)
	if err != nil {
		return nil, err
	}

	hasUpdate := compareVersions(latest, current) > 0
	return &UpdateInfo{
		HasUpdate:   hasUpdate,
		CurrentVer:  c.currentVersion,
		LatestVer:   latest.tag,
		ReleaseNote: releaseMessage(hasUpdate, c.isDocker, latest.tag),
		IsDocker:    c.isDocker,
	}, nil
}

func (c *Checker) fetchLatestVersion(ctx context.Context) (semanticVersion, error) {
	if c.client == nil {
		return semanticVersion{}, errors.New("更新检查 HTTP 客户端未初始化")
	}
	endpoint, err := url.Parse(c.tagsURL)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return semanticVersion{}, fmt.Errorf("Docker Hub 标签地址无效: %q", c.tagsURL)
	}

	seenPages := make(map[string]struct{})
	var latest *semanticVersion
	for endpoint != nil {
		if err := validatePageURL(endpoint, seenPages); err != nil {
			return semanticVersion{}, err
		}
		page, err := c.fetchPage(ctx, endpoint.String())
		if err != nil {
			return semanticVersion{}, err
		}
		latest = latestFromPage(page, latest)
		endpoint, err = resolveNextPage(endpoint, page.Next)
		if err != nil {
			return semanticVersion{}, err
		}
	}
	if latest == nil {
		return semanticVersion{}, errors.New("Docker Hub 未返回可用的稳定版本标签")
	}
	return *latest, nil
}

func (c *Checker) fetchPage(ctx context.Context, endpoint string) (dockerHubTagsPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return dockerHubTagsPage{}, fmt.Errorf("创建更新检查请求失败: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "HiDeck-Update-Checker")

	response, err := c.client.Do(request)
	if err != nil {
		return dockerHubTagsPage{}, fmt.Errorf("请求 Docker Hub 版本信息失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return dockerHubTagsPage{}, fmt.Errorf("Docker Hub 版本接口返回 HTTP %d", response.StatusCode)
	}
	return decodeTagsPage(response.Body)
}

func decodeTagsPage(body io.Reader) (dockerHubTagsPage, error) {
	payload, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return dockerHubTagsPage{}, fmt.Errorf("读取 Docker Hub 版本信息失败: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return dockerHubTagsPage{}, errors.New("Docker Hub 版本响应超过 2 MiB")
	}
	var page dockerHubTagsPage
	if err := json.Unmarshal(payload, &page); err != nil {
		return dockerHubTagsPage{}, fmt.Errorf("解析 Docker Hub 版本信息失败: %w", err)
	}
	return page, nil
}

func latestFromPage(page dockerHubTagsPage, latest *semanticVersion) *semanticVersion {
	for _, result := range page.Results {
		candidate, err := parseStableVersion(strings.TrimSpace(result.Name))
		if err != nil {
			continue
		}
		if latest == nil || compareVersions(candidate, *latest) > 0 || preferredTag(candidate, *latest) {
			copy := candidate
			latest = &copy
		}
	}
	return latest
}

func parseStableVersion(raw string) (semanticVersion, error) {
	match := stableVersionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return semanticVersion{}, errors.New("需要 major.minor 或 major.minor.patch 格式")
	}
	parts := make([]uint64, 3)
	for index := range parts {
		if match[index+1] == "" {
			continue
		}
		value, err := strconv.ParseUint(match[index+1], 10, 64)
		if err != nil {
			return semanticVersion{}, fmt.Errorf("版本号段超出范围: %w", err)
		}
		parts[index] = value
	}
	components := 3
	if match[3] == "" {
		components = 2
	}
	return semanticVersion{major: parts[0], minor: parts[1], patch: parts[2], components: components, tag: strings.TrimSpace(raw)}, nil
}

func compareVersions(left, right semanticVersion) int {
	leftParts := [...]uint64{left.major, left.minor, left.patch}
	rightParts := [...]uint64{right.major, right.minor, right.patch}
	for index := range leftParts {
		if leftParts[index] > rightParts[index] {
			return 1
		}
		if leftParts[index] < rightParts[index] {
			return -1
		}
	}
	return 0
}

func preferredTag(candidate, current semanticVersion) bool {
	return compareVersions(candidate, current) == 0 && candidate.components > current.components
}

func validatePageURL(endpoint *url.URL, seenPages map[string]struct{}) error {
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return fmt.Errorf("Docker Hub 分页地址协议无效: %s", endpoint.Scheme)
	}
	key := endpoint.String()
	if _, exists := seenPages[key]; exists {
		return errors.New("Docker Hub 版本接口返回了循环分页地址")
	}
	seenPages[key] = struct{}{}
	return nil
}

func resolveNextPage(current *url.URL, rawNext string) (*url.URL, error) {
	if strings.TrimSpace(rawNext) == "" {
		return nil, nil
	}
	next, err := url.Parse(rawNext)
	if err != nil {
		return nil, fmt.Errorf("Docker Hub 分页地址无效: %w", err)
	}
	next = current.ResolveReference(next)
	if next.Scheme != current.Scheme || next.Host != current.Host {
		return nil, errors.New("Docker Hub 分页地址跳转到了非预期服务")
	}
	return next, nil
}

func releaseMessage(hasUpdate, isDocker bool, latestVersion string) string {
	if !hasUpdate {
		return "当前版本不低于 Docker Hub 最新稳定版本。"
	}
	if isDocker {
		return fmt.Sprintf("Docker Hub 已发布 HiDeck %s，请拉取最新镜像并重新创建容器。", latestVersion)
	}
	return fmt.Sprintf("Docker Hub 已发布 HiDeck %s；当前构建不支持应用内热替换，请按现有部署方式升级。", latestVersion)
}

func DetectDockerEnvironment() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func ApplyUpdate() error {
	return ErrDisabled
}
