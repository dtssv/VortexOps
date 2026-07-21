// Package buildinfra 的 ImageScanReader 实现 executor.ScanReader，
// 从构建仓储读取镜像扫描结果并解析为 ScanSummary。
package buildinfra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/infrastructure/pipeline/executor"
)

// ImageScanReader 从 vo_images.scan_result 读取 CVE 扫描摘要。
// scan_result 的 JSON 结构预期为 {"summary":{"critical":N,"high":N,"medium":N,"low":N}} 或
// 直接 {"critical":N,...}（兼容 Harbor / Trivy 输出）。
type ImageScanReader struct {
	repo ScanResultRepo
}

// ScanResultRepo 读取镜像扫描结果的最小仓储接口。
type ScanResultRepo interface {
	GetImageByID(ctx context.Context, id int64) (*build.Image, error)
}

// NewImageScanReader 创建扫描结果读取器。
func NewImageScanReader(repo ScanResultRepo) *ImageScanReader {
	return &ImageScanReader{repo: repo}
}

// GetImageScanResult 解析镜像扫描结果为 CVE 计数摘要。
func (r *ImageScanReader) GetImageScanResult(ctx context.Context, imageID int64) (executor.ScanSummary, error) {
	img, err := r.repo.GetImageByID(ctx, imageID)
	if err != nil {
		return executor.ScanSummary{}, fmt.Errorf("get image %d: %w", imageID, err)
	}
	if img.ScanStatus == build.ImgScanSkipped || img.ScanStatus == build.ImgScanPending {
		return executor.ScanSummary{}, nil
	}
	return parseScanResult(img.ScanResult), nil
}

// parseScanResult 从 map[string]any 解析 CVE 计数，兼容嵌套 summary 与扁平两种结构。
func parseScanResult(result map[string]any) executor.ScanSummary {
	if result == nil {
		return executor.ScanSummary{}
	}
	// 优先取嵌套 summary。
	if summary, ok := result["summary"].(map[string]any); ok {
		return extractCounts(summary)
	}
	return extractCounts(result)
}

func extractCounts(m map[string]any) executor.ScanSummary {
	return executor.ScanSummary{
		Critical: toInt(m["critical"]),
		High:     toInt(m["high"]),
		Medium:   toInt(m["medium"]),
		Low:      toInt(m["low"]),
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// 编译期断言。
var _ executor.ScanReader = (*ImageScanReader)(nil)
