package settlement

import (
	"math"
	"strings"

	"streetlight/internal/apperr"
)

// errInvalidPeriod 返回非法月份参数错误。
func errInvalidPeriod(value string) error {
	return apperr.BadRequest("月份格式应为 YYYY-MM, 当前值: %s", strings.TrimSpace(value))
}

// round2 保留两位小数, 避免浮点累计误差。
func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
