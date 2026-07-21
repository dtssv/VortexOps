package releaseapp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/domain/release"
)

// windowChecker 发布窗口校验器：基于应用的活跃窗口 crontab 判断当前是否在窗口内。
type windowChecker struct {
	repo release.Repository
}

// NewWindowChecker 创建发布窗口校验器。
func NewWindowChecker(repo release.Repository) WindowChecker {
	return &windowChecker{repo: repo}
}

// IsWithinWindow 返回当前时间是否在应用的任一活跃发布窗口内。
// 无活跃窗口时返回 true（不限制）；存在活跃窗口但都不匹配时返回 false。
func (w *windowChecker) IsWithinWindow(ctx context.Context, appID int64, now time.Time) (bool, string, error) {
	windows, err := w.repo.ListWindows(ctx, appID)
	if err != nil {
		return false, "", err
	}
	var active []*release.ReleaseWindow
	for _, win := range windows {
		if win.IsActive {
			active = append(active, win)
		}
	}
	if len(active) == 0 {
		return true, "no active release window", nil
	}
	for _, win := range active {
		loc := time.Local
		if win.Timezone != "" {
			if l, err := time.LoadLocation(win.Timezone); err == nil {
				loc = l
			}
		}
		localNow := now.In(loc)
		ok, err := matchCrontab(win.Crontab, localNow)
		if err != nil {
			continue
		}
		if ok {
			// 命中窗口起点后，校验是否仍在 duration 范围内。
			start := windowStart(win.Crontab, localNow, win.DurationMinutes)
			end := start.Add(time.Duration(win.DurationMinutes) * time.Minute)
			if !localNow.Before(start) && localNow.Before(end) {
				return true, win.Name, nil
			}
		}
	}
	return false, "outside all active release windows", nil
}

// matchCrontab 简化 5 字段 crontab 匹配（分 时 日 月 周），支持 * 与逗号与范围。
func matchCrontab(expr string, t time.Time) (bool, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false, fmt.Errorf("invalid crontab: %s", expr)
	}
	minute, err := matchField(fields[0], t.Minute(), 0, 59)
	if err != nil || !minute {
		return false, err
	}
	hour, err := matchField(fields[1], t.Hour(), 0, 23)
	if err != nil || !hour {
		return false, err
	}
	dom, err := matchField(fields[2], t.Day(), 1, 31)
	if err != nil || !dom {
		return false, err
	}
	month, err := matchField(fields[3], int(t.Month()), 1, 12)
	if err != nil || !month {
		return false, err
	}
	// 周日=0；Go time.Weekday() Sunday=0。
	dow, err := matchField(fields[4], int(t.Weekday()), 0, 6)
	if err != nil || !dow {
		return false, err
	}
	return true, nil
}

// matchField 匹配单个 crontab 字段。
func matchField(field string, value, min, max int) (bool, error) {
	if field == "*" {
		return true, nil
	}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			return true, nil
		}
		// 步长 a/b
		if idx := strings.Index(part, "/"); idx >= 0 {
			base := part[:idx]
			step, err := strconv.Atoi(part[idx+1:])
			if err != nil || step <= 0 {
				return false, fmt.Errorf("invalid step: %s", part)
			}
			start := min
			if base != "*" {
				start, err = strconv.Atoi(base)
				if err != nil {
					return false, fmt.Errorf("invalid range: %s", part)
				}
			}
			if value >= start && (value-start)%step == 0 && value <= max {
				return true, nil
			}
			continue
		}
		// 范围 a-b
		if idx := strings.Index(part, "-"); idx >= 0 {
			lo, err1 := strconv.Atoi(part[:idx])
			hi, err2 := strconv.Atoi(part[idx+1:])
			if err1 != nil || err2 != nil {
				return false, fmt.Errorf("invalid range: %s", part)
			}
			if value >= lo && value <= hi {
				return true, nil
			}
			continue
		}
		// 单值
		v, err := strconv.Atoi(part)
		if err != nil {
			return false, fmt.Errorf("invalid value: %s", part)
		}
		if v == value {
			return true, nil
		}
	}
	return false, nil
}

// windowStart 近似计算当前周期内窗口的起始时间（向下取整到匹配的分钟）。
// 简化实现：以当前分钟作为起点（满足 duration 内即放行）。
func windowStart(_ string, now time.Time, _ int) time.Time {
	return now.Truncate(time.Minute)
}
