// Package monitoringapp 提供容器监控应用服务：查询 Prometheus 指标、评估告警规则。
// Prometheus 地址从系统设置 monitoring.prometheus_url 读取，支持按集群 Annotation 覆盖。
package monitoringapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/vortexops/vortexops/internal/application/alertapp"
	"github.com/vortexops/vortexops/internal/application/systemapp"
	"github.com/vortexops/vortexops/internal/domain/alert"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 监控应用服务。
type Service struct {
	alertSvc  *alertapp.Service
	systemSvc *systemapp.Service
	http      *http.Client
}

// New 创建监控服务。
func New(alertSvc *alertapp.Service, systemSvc *systemapp.Service) *Service {
	return &Service{
		alertSvc:  alertSvc,
		systemSvc: systemSvc,
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

// StartAlertEvaluator 启动后台告警规则评估 worker，每 interval 周期评估一次。
// 若 interval <= 0 则不启动。Prometheus 未配置时跳过该周期。
func (s *Service) StartAlertEvaluator(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Prometheus 未配置时跳过（EvaluateRules 内部会失败）。
				_, _ = s.EvaluateRules(ctx)
			}
		}
	}()
}

// prometheusURL 从系统设置读取 Prometheus 地址。
// 未配置时返回 NotFound，调用方应转译为可读的配置缺失提示。
func (s *Service) prometheusURL(ctx context.Context) (string, error) {
	setting, err := s.systemSvc.Get(ctx, "monitoring.prometheus_url")
	if err != nil {
		return "", err
	}
	if setting == nil {
		return "", apperr.NotFound("system setting", "monitoring.prometheus_url")
	}
	v, ok := setting.Value.(string)
	if !ok || v == "" {
		return "", apperr.NotFound("system setting", "monitoring.prometheus_url")
	}
	return v, nil
}

// wrapPrometheusErr 把底层网络/HTTP 错误转译为可读的应用错误。
// 区分 DNS 解析失败（通常是 URL 配置错误）、连接被拒（Prometheus 未启动）
// 与其他 HTTP 错误，便于前端展示与运维定位。
func wrapPrometheusErr(baseURL, op string, err error) error {
	if err == nil {
		return nil
	}
	// url.Error 携带网络层细节。
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		var dnsErr *net.DNSError
		if errors.As(urlErr.Err, &dnsErr) {
			return apperr.Internal(fmt.Sprintf(
				"cannot resolve Prometheus host %q (configured URL: %s); please verify monitoring.prometheus_url in system settings",
				dnsErr.Name, baseURL,
			), err)
		}
		// 连接被拒 / 超时 / 网络不可达等。
		return apperr.Internal(fmt.Sprintf(
			"cannot reach Prometheus at %s during %s: %v; please verify the service is running and the URL is correct",
			baseURL, op, urlErr.Err,
		), err)
	}
	return apperr.Internal(fmt.Sprintf("prometheus %s failed: %v", op, err), err)
}

// QueryResult Prometheus instant query结果。
type QueryResult struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"` // [timestamp, "value"]
}

// Query 执行 PromQL 即时查询。
func (s *Service) Query(ctx context.Context, promQL string) ([]QueryResult, error) {
	base, err := s.prometheusURL(ctx)
	if err != nil {
		return nil, err
	}
	u := base + "/api/v1/query?query=" + url.QueryEscape(promQL)
	respData, err := s.doRequest(ctx, u, base, "instant query")
	if err != nil {
		return nil, err
	}
	var apiResp struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []QueryResult `json:"result"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respData, &apiResp); err != nil {
		return nil, apperr.Internal("parse prometheus response", err)
	}
	if apiResp.Status != "success" {
		return nil, apperr.Internal("prometheus query failed: "+apiResp.Error, nil)
	}
	return apiResp.Data.Result, nil
}

// QueryRangeResult Prometheus range query结果点。
type QueryRangeResult struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"` // [[timestamp, "value"], ...]
}

// QueryRangeInput 范围查询输入。
type QueryRangeInput struct {
	PromQL string
	Start  time.Time
	End    time.Time
	Step   string // 如 "15s", "1m", "5m"
}

// QueryRange 执行 PromQL 范围查询。
func (s *Service) QueryRange(ctx context.Context, in QueryRangeInput) ([]QueryRangeResult, error) {
	base, err := s.prometheusURL(ctx)
	if err != nil {
		return nil, err
	}
	if in.Step == "" {
		in.Step = "1m"
	}
	u := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=%s",
		base, url.QueryEscape(in.PromQL), in.Start.Unix(), in.End.Unix(), in.Step)
	respData, err := s.doRequest(ctx, u, base, "range query")
	if err != nil {
		return nil, err
	}
	var apiResp struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []QueryRangeResult `json:"result"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respData, &apiResp); err != nil {
		return nil, apperr.Internal("parse prometheus response", err)
	}
	if apiResp.Status != "success" {
		return nil, apperr.Internal("prometheus query_range failed: "+apiResp.Error, nil)
	}
	return apiResp.Data.Result, nil
}

// doRequest 执行 HTTP GET 并返回响应体。op 用于错误消息，baseURL 用于错误转译。
func (s *Service) doRequest(ctx context.Context, u, baseURL, op string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, apperr.Internal(fmt.Sprintf("create prometheus %s request", op), err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, wrapPrometheusErr(baseURL, op, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, apperr.Internal("read prometheus response", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apperr.Internal(fmt.Sprintf("prometheus returned status %d during %s: %s", resp.StatusCode, op, string(body)), nil)
	}
	return body, nil
}

// EvaluateRules 评估所有启用的告警规则，触发阈值则记录事件。
// 此方法由告警评估 worker 周期调用。
func (s *Service) EvaluateRules(ctx context.Context) (int, error) {
	enabled := true
	rules, _, err := s.alertSvc.ListRules(ctx, alert.RuleQuery{Enabled: &enabled, Limit: 500})
	if err != nil {
		return 0, err
	}
	triggered := 0
	for _, rule := range rules {
		ok, err := s.evaluateRule(ctx, rule)
		if err != nil {
			continue
		}
		if ok {
			triggered++
		}
	}
	return triggered, nil
}

// evaluateRule 评估单条规则：查询指标当前值，与阈值比较。
// 触发时落库一条 firing 告警事件。
func (s *Service) evaluateRule(ctx context.Context, rule *alert.Rule) (bool, error) {
	results, err := s.Query(ctx, rule.Metric)
	if err != nil || len(results) == 0 {
		return false, err
	}
	// 取第一个结果系列的最新值。
	valStr, ok := results[0].Value[1].(string)
	if !ok {
		return false, nil
	}
	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return false, nil
	}
	threshold := 0.0
	if rule.Threshold != nil {
		threshold = *rule.Threshold
	}
	triggered := false
	switch rule.Condition {
	case alert.CondGT:
		triggered = val > threshold
	case alert.CondGTE:
		triggered = val >= threshold
	case alert.CondLT:
		triggered = val < threshold
	case alert.CondLTE:
		triggered = val <= threshold
	case alert.CondEQ:
		triggered = val == threshold
	case alert.CondNEQ:
		triggered = val != threshold
	}
	if !triggered {
		return false, nil
	}
	// 记录 firing 事件（评估器为系统行为，createdBy=0）。
	evt := &alert.Event{
		RuleID:       rule.ID,
		Scope:        rule.Scope,
		ScopeID:      rule.ScopeID,
		Severity:     rule.Severity,
		Status:       alert.EventFiring,
		Message:      fmt.Sprintf("%s 触发：%s 当前值 %.4f %s %.4f", rule.Name, rule.Metric, val, rule.Condition, threshold),
		CurrentValue: &val,
		FiredAt:      time.Now(),
	}
	_ = s.alertSvc.CreateEvent(ctx, evt)
	return true, nil
}
