// Package jenkins 实现 Jenkins REST 客户端，用于触发构建、查询状态、拉取 console log。
// 使用 Jenkins REST API（无需插件）：/job/{name}/buildWithParameters、/job/{name}/lastBuild/api/json。
// 鉴权：用户名 + API Token（Basic Auth），凭证 payload 为 JSON {username, api_token}。
package jenkins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/domain/build"
)

// Client Jenkins REST 客户端。
type Client struct {
	baseURL  string
	username string
	apiToken string
	http     *http.Client
	// crumb 缓存：Jenkins 默认开启 CSRF crumb 保护，所有 POST 必须带 Jenkins-Crumb 头。
	// 首次 POST 时懒加载，缓存到 crumbField/crumbValue；403 时清空重试一次。
	crumbField string
	crumbValue string
}

// New 创建 Jenkins 客户端。rawCredential 为凭证解密后的 JSON。
func New(baseURL string, rawCredential []byte) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("jenkins url is required")
	}
	var cred struct {
		Username  string `json:"username"`
		APIToken  string `json:"api_token"`
	}
	if err := json.Unmarshal(rawCredential, &cred); err != nil {
		return nil, fmt.Errorf("parse jenkins credential: %w", err)
	}
	if cred.Username == "" || cred.APIToken == "" {
		return nil, errors.New("jenkins credential requires username and api_token")
	}
	// Cookie jar：Jenkins CSRF crumb 与 session 绑定，POST 必须带获取 crumb 时的 session cookie，
	// 否则即使带 Jenkins-Crumb 头仍 403。jar 自动维护整个 Client 生命周期的 cookie。
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	return &Client{
		baseURL:  baseURL,
		username: cred.Username,
		apiToken: cred.APIToken,
		http:     &http.Client{Timeout: 30 * time.Second, Jar: jar},
	}, nil
}

// fetchCrumb 从 Jenkins 获取 CSRF crumb（若 crumb issuer 未启用则跳过）。
// Jenkins 默认开启 DefaultCrumbIssuer，POST 请求必须带 crumb，否则 403。
func (c *Client) fetchCrumb(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/crumbIssuer/api/json", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("fetch crumb: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// crumb issuer 未启用，无需 crumb。
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch crumb: unexpected status %d", resp.StatusCode)
	}
	var crumb struct {
		Crumb             string `json:"crumb"`
		CrumbRequestField string `json:"crumbRequestField"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&crumb); err != nil {
		return fmt.Errorf("decode crumb: %w", err)
	}
	c.crumbField = crumb.CrumbRequestField
	c.crumbValue = crumb.Crumb
	return nil
}

// setCrumb 为 POST 请求附加 Jenkins CSRF crumb 头（已缓存则直接用，否则懒加载）。
func (c *Client) setCrumb(ctx context.Context, req *http.Request) error {
	if c.crumbField == "" || c.crumbValue == "" {
		if err := c.fetchCrumb(ctx); err != nil {
			return err
		}
	}
	if c.crumbField != "" && c.crumbValue != "" {
		req.Header.Set(c.crumbField, c.crumbValue)
	}
	return nil
}

// TriggerBuild 触发参数化构建，返回 Jenkins 队列项 ID。
func (c *Client) TriggerBuild(ctx context.Context, jobName string, params map[string]string) (string, error) {
	if jobName == "" {
		return "", errors.New("job name is required")
	}
	// jobName 可能含文件夹路径（folder/job），需 URL 编码每段。
	encoded := encodeJobPath(jobName)
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	endpoint := fmt.Sprintf("%s/%s/buildWithParameters", c.baseURL, encoded)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := c.setCrumb(ctx, req); err != nil {
		return "", fmt.Errorf("trigger build: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("trigger build: %w", err)
	}
	defer resp.Body.Close()
	// 403 可能是 crumb 过期：清空缓存重试一次。
	if resp.StatusCode == http.StatusForbidden {
		c.crumbField, c.crumbValue = "", ""
		req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		req2.SetBasicAuth(c.username, c.apiToken)
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if err := c.setCrumb(ctx, req2); err == nil {
			resp.Body.Close()
			resp, err = c.http.Do(req2)
			if err != nil {
				return "", fmt.Errorf("trigger build: %w", err)
			}
			defer resp.Body.Close()
		}
	}
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("trigger build: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	// 队列项 ID 在 Location 头的 URL 末段。
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("trigger build: missing Location header")
	}
	return extractQueueID(loc), nil
}

// buildInfo Jenkins 构建信息。
type buildInfo struct {
	Number  int    `json:"number"`
	Result  string `json:"result"`
	Building bool  `json:"building"`
}

// GetBuildStatus 查询构建状态。
func (c *Client) GetBuildStatus(ctx context.Context, jobName string, buildNumber int) (build.BuildStatus, bool, error) {
	encoded := encodeJobPath(jobName)
	endpoint := fmt.Sprintf("%s/%s/%d/api/json?tree=number,result,building", c.baseURL, encoded, buildNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false, err
	}
	req.SetBasicAuth(c.username, c.apiToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("get build status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return build.BuildQueued, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("get build status: unexpected status %d", resp.StatusCode)
	}
	var info buildInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", false, fmt.Errorf("decode build info: %w", err)
	}
	if info.Building {
		return build.BuildRunning, true, nil
	}
	return mapJenkinsResult(info.Result), false, nil
}

// GetLastBuildNumber 返回 job 最新构建号；job 不存在或无构建时返回 (0, nil)。
// 用于队列项已被 Jenkins GC（queue/item/{id} 返回 404）后回溯构建号。
func (c *Client) GetLastBuildNumber(ctx context.Context, jobName string) (int, error) {
	if jobName == "" {
		return 0, errors.New("job name is required")
	}
	encoded := encodeJobPath(jobName)
	endpoint := fmt.Sprintf("%s/%s/api/json?tree=lastBuild[number]", c.baseURL, encoded)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get last build number: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("get last build number: unexpected status %d", resp.StatusCode)
	}
	var info struct {
		LastBuild struct {
			Number int `json:"number"`
		} `json:"lastBuild"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return 0, fmt.Errorf("decode last build: %w", err)
	}
	return info.LastBuild.Number, nil
}

// GetQueueItemBuildNumber 查询队列项是否已被分配 Jenkins 构建号。
// Jenkins 触发后先入队，返回 queue/item/{id} 的 Location；当队列项被调度执行时，
// 该 API 的响应会带上 `executable.url`（指向 /job/{name}/{number}/），从中解析出构建号。
// 仍在排队（未分配）时返回 (0, false, nil)；已完成且未留下构建号（罕见，如被丢弃）返回 (0, false, nil)。
func (c *Client) GetQueueItemBuildNumber(ctx context.Context, queueID string) (int, bool, error) {
	if queueID == "" {
		return 0, false, errors.New("queue id is required")
	}
	endpoint := fmt.Sprintf("%s/queue/item/%s/api/json?tree=executable[number],why", c.baseURL, url.PathEscape(queueID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, false, err
	}
	req.SetBasicAuth(c.username, c.apiToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, false, fmt.Errorf("get queue item: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// 队列项已离开队列但未留下 executable（被取消/丢弃）。
		return 0, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return 0, false, fmt.Errorf("get queue item: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	var qi struct {
		Executable struct {
			Number int `json:"number"`
		} `json:"executable"`
		Why string `json:"why"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&qi); err != nil {
		return 0, false, fmt.Errorf("decode queue item: %w", err)
	}
	if qi.Executable.Number == 0 {
		// 仍在排队等待调度。
		return 0, false, nil
	}
	return qi.Executable.Number, true, nil
}

// GetConsoleLog 拉取 console log（支持从 start 字节增量拉取）。
// 返回日志文本与是否还有更多（Jenkins consoleText 无 hasMore 标记，这里通过返回长度判断）。
func (c *Client) GetConsoleLog(ctx context.Context, jobName string, buildNumber int, start int64) (string, bool, error) {
	encoded := encodeJobPath(jobName)
	endpoint := fmt.Sprintf("%s/%s/%d/consoleText", c.baseURL, encoded, buildNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false, err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("get console log: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return "", false, fmt.Errorf("get console log: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, err
	}
	// 简单判断：返回内容 > 0 即可能有更多（轮询方按时间增量拉）。
	return string(body), len(body) > 0, nil
}

// StopBuild 停止正在运行的构建。
func (c *Client) StopBuild(ctx context.Context, jobName string, buildNumber int) error {
	encoded := encodeJobPath(jobName)
	endpoint := fmt.Sprintf("%s/%s/%d/stop", c.baseURL, encoded, buildNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(nil))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	if err := c.setCrumb(ctx, req); err != nil {
		return fmt.Errorf("stop build crumb: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("stop build: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("stop build: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// EnsureJob 确保 job 存在，不存在则按 configXML 创建。
// jobName 形如 "vortexops/app-1"：先探测 folder（vortexops），404 则创建；
// 再探测 job（app-1），404 则用 configXML 创建参数化 Pipeline job。
// 已存在（200）时快速短路，幂等可重复调用。
func (c *Client) EnsureJob(ctx context.Context, jobName, configXML string) error {
	if jobName == "" {
		return errors.New("job name is required")
	}
	parts := strings.Split(jobName, "/")
	// folder 路径：除最后一段外都是 folder（支持嵌套 folder，常见为单层 vortexops/app-1）。
	folders := parts[:len(parts)-1]
	job := parts[len(parts)-1]
	if job == "" {
		return errors.New("invalid job name: empty job segment")
	}

	// 逐级确保 folder 存在。
	parentPath := ""
	for _, f := range folders {
		if f == "" {
			continue
		}
		if err := c.ensureFolder(ctx, parentPath, f); err != nil {
			return fmt.Errorf("ensure folder %s: %w", f, err)
		}
		if parentPath == "" {
			parentPath = f
		} else {
			parentPath = parentPath + "/" + f
		}
	}

	// 探测 job 是否存在。
	jobEndpoint := fmt.Sprintf("%s/%s/api/json", c.baseURL, encodeJobPath(jobName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jobEndpoint, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("probe job: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		// job 已存在：更新 config.xml，确保 Pipeline 脚本为最新（修复历史 job 残留旧脚本）。
		return c.updateJobConfig(ctx, jobName, configXML)
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("probe job: unexpected status %d", resp.StatusCode)
	}

	// job 不存在，创建。
	createEndpoint := fmt.Sprintf("%s/createItem?name=%s", c.baseURL, url.QueryEscape(job))
	if parentPath != "" {
		createEndpoint = fmt.Sprintf("%s/%s/createItem?name=%s", c.baseURL, encodeJobPath(parentPath), url.QueryEscape(job))
	}
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, strings.NewReader(configXML))
	if err != nil {
		return err
	}
	createReq.SetBasicAuth(c.username, c.apiToken)
	createReq.Header.Set("Content-Type", "application/xml; charset=utf-8")
	if err := c.setCrumb(ctx, createReq); err != nil {
		return fmt.Errorf("create job crumb: %w", err)
	}
	createResp, err := c.http.Do(createReq)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(createResp.Body, 1024))
		return fmt.Errorf("create job: unexpected status %d: %s", createResp.StatusCode, string(body))
	}
	return nil
}

// updateJobConfig 用最新 config.xml 覆盖已存在 job 的配置（POST <jobPath>/config.xml）。
func (c *Client) updateJobConfig(ctx context.Context, jobName, configXML string) error {
	endpoint := fmt.Sprintf("%s/%s/config.xml", c.baseURL, encodeJobPath(jobName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(configXML))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	if err := c.setCrumb(ctx, req); err != nil {
		return fmt.Errorf("update job config crumb: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("update job config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("update job config: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ensureFolder 确保单个 folder 存在。parentPath 为父 folder 路径（顶层为空）。
func (c *Client) ensureFolder(ctx context.Context, parentPath, folder string) error {
	folderPath := folder
	if parentPath != "" {
		folderPath = parentPath + "/" + folder
	}
	probeEndpoint := fmt.Sprintf("%s/%s/api/json", c.baseURL, encodeJobPath(folderPath))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeEndpoint, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("probe folder: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("probe folder: unexpected status %d", resp.StatusCode)
	}
	// 创建 folder（CloudBees Folder 插件，lts 默认含）。
	createEndpoint := fmt.Sprintf("%s/createItem?name=%s", c.baseURL, url.QueryEscape(folder))
	if parentPath != "" {
		createEndpoint = fmt.Sprintf("%s/%s/createItem?name=%s", c.baseURL, encodeJobPath(parentPath), url.QueryEscape(folder))
	}
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, strings.NewReader(folderConfigXML))
	if err != nil {
		return err
	}
	createReq.SetBasicAuth(c.username, c.apiToken)
	createReq.Header.Set("Content-Type", "application/xml; charset=utf-8")
	if err := c.setCrumb(ctx, createReq); err != nil {
		return fmt.Errorf("create folder crumb: %w", err)
	}
	createResp, err := c.http.Do(createReq)
	if err != nil {
		return fmt.Errorf("create folder: %w", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(createResp.Body, 1024))
		return fmt.Errorf("create folder: unexpected status %d: %s", createResp.StatusCode, string(body))
	}
	return nil
}

// folderConfigXML 创建 CloudBees Folder 所需的最小 config.xml。
const folderConfigXML = `<?xml version='1.1' encoding='UTF-8'?>
<com.cloudbees.hudson.plugins.folder.Folder plugin="cloudbees-folder">
  <description></description>
  <properties/>
  <folderViews/>
  <healthMetrics/>
</com.cloudbees.hudson.plugins.folder.Folder>`

func mapJenkinsResult(result string) build.BuildStatus {
	switch strings.ToUpper(result) {
	case "SUCCESS":
		return build.BuildSuccess
	case "FAILURE":
		return build.BuildFailed
	case "ABORTED":
		return build.BuildCanceled
	case "UNSTABLE":
		return build.BuildFailed
	default:
		return build.BuildQueued
	}
}

// encodeJobPath 编码可能含文件夹的 job 路径（folder/sub/job → folder/job/sub/job）。
func encodeJobPath(name string) string {
	parts := strings.Split(name, "/")
	encoded := make([]string, 0, len(parts)*2)
	for _, p := range parts {
		if p == "" {
			continue
		}
		encoded = append(encoded, "job", url.PathEscape(p))
	}
	return strings.Join(encoded, "/")
}

// extractQueueID 从队列项 Location URL 提取 ID。
// URL 形如 https://jenkins/queue/item/123/
func extractQueueID(loc string) string {
	loc = strings.TrimRight(loc, "/")
	idx := strings.LastIndex(loc, "/")
	if idx < 0 {
		return loc
	}
	return loc[idx+1:]
}
