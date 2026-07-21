package diagnosisapp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildIntentPrompt 构造意图识别提示。
// 让 LLM 输出 JSON：{category, reasoning, tools:[{name, args}]}。
func buildIntentPrompt(query string) string {
	tools := AvailableTools()
	toolList := make([]string, 0, len(tools))
	for _, t := range tools {
		toolList = append(toolList, fmt.Sprintf("- %s: %s", t.Name, t.Description))
	}
	return fmt.Sprintf(`你是 VortexOps 平台的意图识别助手。请分析用户的问题，识别其意图并推荐应调用的工具。

## 用户问题
%s

## 平台可用工具
%s

## 工具参数说明
- get_build / get_build_logs / list_build_steps: 需要 build_id
- find_failed_builds: 需要 app_id
- get_group / list_group_pods: 需要 group_id
- find_group_by_name: 需要 name（分组名）
- get_pod_logs / list_pod_events / describe_pod: 需要 cluster_id, namespace, pod；get_pod_logs 可选 container, tail

## 意图类别（category）
- build_failure: 镜像构建失败、构建报错、构建卡住、镜像推送失败
- pod_failure: Pod 启动失败、CrashLoopBackOff、Pod 崩溃、应用未就绪
- release_issue: 发布失败、发布卡住、回滚、滚动更新问题
- k8s_ops: K8s 运维问题（Pending、资源不足、网络、配置、节点）
- general_question: 通用问答（功能咨询、操作指引、概念解释）

## 推理原则
1. 用户可能不会明确说出失败类型，要从问题描述中推断（如「构建卡住了」→build_failure，「Pod 一直起不来」→pod_failure）
2. 实体未明确时不要凭空编造 ID；可推荐 find_group_by_name 等模糊查找工具
3. 工具调用要克制：只推荐对回答问题有帮助的工具，避免无意义的全量调用
4. 若问题不需要工具（如纯概念咨询），tools 返回空数组
5. 优先推荐 search_faq 检索知识库，命中即无需调用其它工具

## 输出格式（严格 JSON，不要 markdown 代码块）
{
  "category": "<意图类别>",
  "reasoning": "<1-2 句推理过程，说明为何这样判断>",
  "tools": [
    {"name": "<工具名>", "args": {"key": "value"}}
  ]
}

请直接输出 JSON，不要任何额外文字。`, query, strings.Join(toolList, "\n"))
}

// parseIntent 解析 LLM 返回的意图 JSON。
// 容错：去除 markdown 代码块包裹、截取首个 JSON 对象。
func parseIntent(raw string) (*Intent, error) {
	cleaned := cleanJSONResponse(raw)
	var intent Intent
	if err := json.Unmarshal([]byte(cleaned), &intent); err != nil {
		return nil, fmt.Errorf("parse intent json: %w (raw: %s)", err, truncate(raw, 200))
	}
	// 校正类别。
	switch intent.Category {
	case "build_failure", "pod_failure", "release_issue", "k8s_ops", "general_question":
		// ok
	default:
		intent.Category = "general_question"
	}
	return &intent, nil
}

// cleanJSONResponse 清理 LLM 返回的 JSON：
// - 去除 ```json ... ``` 代码块包裹
// - 截取首个 { 到最后一个 } 之间的内容
func cleanJSONResponse(s string) string {
	s = strings.TrimSpace(s)
	// 去除 markdown 代码块。
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// 截取首个 JSON 对象。
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
