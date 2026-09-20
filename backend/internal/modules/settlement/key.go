package settlement

import "strings"

// teamPeriodKey 生成班组与月份的复合键, 用于锁定状态判断与批量索引。
func teamPeriodKey(team, period string) string {
	return team + "|" + period
}

// splitTeamPeriodKey 解析复合键。
func splitTeamPeriodKey(key string) (team, period string, ok bool) {
	parts := strings.SplitN(key, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
