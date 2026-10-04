package cbba

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// 报告必须生成为单一自包含 HTML：内嵌数据可解析、不引用任何外部资源。
func TestWriteReportSelfContained(t *testing.T) {
	cfg := Config{
		Robots: []Robot{
			{ID: 0, X: 0, Y: 0, Capacity: 3},
			{ID: 1, X: 10, Y: 0, Capacity: 3},
		},
		Tasks: []Task{
			{ID: 0, X: 2, Y: 0, Reward: 100},
			{ID: 1, X: 8, Y: 0, Reward: 100},
			{ID: 2, X: 10000, Y: 10000, Reward: 1}, // 无法承接
		},
		Lambda: 1,
		Loss:   BlackoutLoss(3),
	}
	res := Run(cfg)
	if !res.Converged {
		t.Fatal("未收敛")
	}
	var buf bytes.Buffer
	if err := WriteReport(&buf, cfg, res); err != nil {
		t.Fatalf("生成报告失败: %v", err)
	}
	html := buf.String()
	if !strings.HasPrefix(html, "<!DOCTYPE html>") {
		t.Fatal("不是合法的 HTML 文档")
	}
	// 不得引用任何外部资源（框架、CDN、图形库）。SVG 命名空间 URI 是
	// 标识符而非网络请求，不在此列。
	for _, ext := range []string{"<script src", "<link ", "@import", "fetch(", "XMLHttpRequest", "cdn."} {
		if strings.Contains(html, ext) {
			t.Fatalf("报告包含外部引用 %q（要求完全自包含）", ext)
		}
	}
	// 内嵌的 JSON 数据必须可解析。
	const marker = "const DATA = "
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatal("找不到内嵌数据")
	}
	j := strings.Index(html[i+len(marker):], ";")
	var data map[string]any
	if err := json.Unmarshal([]byte(html[i+len(marker):i+len(marker)+j]), &data); err != nil {
		t.Fatalf("内嵌 JSON 无法解析: %v", err)
	}
	if data["converged"] != true {
		t.Fatal("内嵌数据中的收敛标志不正确")
	}
	if ua, ok := data["unassignable"].([]any); !ok || len(ua) != 1 {
		t.Fatalf("内嵌数据中的无法承接任务不正确: %v", data["unassignable"])
	}
}
