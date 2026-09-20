package settlement

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"streetlight/internal/modules/repair"
	"streetlight/pkg/pagination"
)

// LedgerFilter 费用台账查询条件, 仅统计已完工的维修记录。
type LedgerFilter struct {
	Keyword    string
	RepairTeam string
	Period     string
	From       *time.Time
	To         *time.Time
	// ApprovedKeys 为已审核通过的 班组|月份 集合: settled=yes 只看这些, settled=no 排除这些。
	ApprovedKeys map[string]bool
	SettledOnly  bool
	PendingOnly  bool
}

// ledgerSortSpec 定义费用台账允许的排序字段白名单。
var ledgerSortSpec = pagination.SortSpec{
	Allowed: map[string]string{
		"repair_no":   "repair_no",
		"repair_team": "repair_team",
		"repairman":   "repairman",
		"finished_at": "finished_at",
		"cost":        "cost",
		"created_at":  "created_at",
	},
	Default: "finished_at",
}

// applyLedgerFilter 拼装台账查询条件, 仅保留已完工且有完工期的记录。
func applyLedgerFilter(statement *gorm.DB, filter LedgerFilter) *gorm.DB {
	statement = statement.Where("status = ? AND finished_at IS NOT NULL", repair.StatusFinished)
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		statement = statement.Where(
			"repair_no LIKE ? OR fault_no LIKE ? OR lamp_code LIKE ? OR repairman LIKE ?",
			like, like, like, like,
		)
	}
	if value := strings.TrimSpace(filter.RepairTeam); value != "" {
		statement = statement.Where("repair_team = ?", value)
	}
	if filter.From != nil {
		statement = statement.Where("finished_at >= ?", *filter.From)
	}
	if filter.To != nil {
		statement = statement.Where("finished_at < ?", *filter.To)
	}
	statement = applySettledScope(statement, filter)
	return statement
}

// applySettledScope 用可移植的区间条件表达"已结算月份 / 未结算月份"筛选, 兼容 sqlite 与 postgres。
func applySettledScope(statement *gorm.DB, filter LedgerFilter) *gorm.DB {
	if !filter.SettledOnly && !filter.PendingOnly {
		return statement
	}

	clauses := make([]string, 0, len(filter.ApprovedKeys))
	args := make([]any, 0, len(filter.ApprovedKeys)*3)
	for key := range filter.ApprovedKeys {
		team, period, ok := splitTeamPeriodKey(key)
		if !ok {
			continue
		}
		from, err := parsePeriod(period)
		if err != nil {
			continue
		}
		clauses = append(clauses, "(repair_team = ? AND finished_at >= ? AND finished_at < ?)")
		args = append(args, team, from, from.AddDate(0, 1, 0))
	}

	if len(clauses) == 0 {
		if filter.SettledOnly {
			// 没有任何已结算月份时, 已结算清单必为空。
			statement = statement.Where("1 = 0")
		}
		return statement
	}

	combined := strings.Join(clauses, " OR ")
	if filter.PendingOnly {
		statement = statement.Where("NOT ("+combined+")", args...)
	} else {
		statement = statement.Where("("+combined+")", args...)
	}
	return statement
}

// CollectFinishedRepairs 归集指定班组与完工月份的全部已完工维修记录, 按完工时间正序。
func (r *Repository) CollectFinishedRepairs(ctx context.Context, team, period string) ([]repair.Repair, error) {
	from, err := parsePeriod(period)
	if err != nil {
		return nil, err
	}
	to := from.AddDate(0, 1, 0)
	entities := make([]repair.Repair, 0)
	err = r.session(ctx).
		Where("status = ? AND finished_at IS NOT NULL", repair.StatusFinished).
		Where("repair_team = ? AND finished_at >= ? AND finished_at < ?", team, from, to).
		Order("finished_at ASC, id ASC").
		Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("归集班组月份维修费用失败: %w", err)
	}
	return entities, nil
}

// CountFinishedRepairs 统计指定班组与完工月份的已完工维修记录数量。
func (r *Repository) CountFinishedRepairs(ctx context.Context, team, period string) (int64, error) {
	from, err := parsePeriod(period)
	if err != nil {
		return 0, err
	}
	var total int64
	err = r.session(ctx).Model(&repair.Repair{}).
		Where("status = ? AND finished_at IS NOT NULL", repair.StatusFinished).
		Where("repair_team = ? AND finished_at >= ? AND finished_at < ?", team, from, from.AddDate(0, 1, 0)).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计班组月份维修记录失败: %w", err)
	}
	return total, nil
}

// LedgerList 分页查询费用台账(已完工维修记录)。
func (r *Repository) LedgerList(ctx context.Context, filter LedgerFilter, page pagination.Query) ([]repair.Repair, int64, error) {
	base := func() *gorm.DB {
		return applyLedgerFilter(r.session(ctx).Model(&repair.Repair{}), filter)
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计费用台账失败: %w", err)
	}

	entities := make([]repair.Repair, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("查询费用台账失败: %w", err)
	}
	return entities, total, nil
}

// LedgerTotals 按当前过滤条件汇总台账数量与金额。
func (r *Repository) LedgerTotals(ctx context.Context, filter LedgerFilter) (int64, float64, error) {
	var result struct {
		Total  int64
		Amount float64
	}
	err := applyLedgerFilter(r.session(ctx).Model(&repair.Repair{}), filter).
		Select("COUNT(*) AS total, COALESCE(SUM(cost), 0) AS amount").
		Scan(&result).Error
	if err != nil {
		return 0, 0, fmt.Errorf("汇总费用台账失败: %w", err)
	}
	return result.Total, result.Amount, nil
}

// DistinctTeams 返回已完工维修记录中出现过的班组(非空)。
func (r *Repository) DistinctTeams(ctx context.Context) ([]string, error) {
	values := make([]string, 0)
	err := r.session(ctx).Model(&repair.Repair{}).
		Where("status = ? AND finished_at IS NOT NULL AND repair_team <> ''", repair.StatusFinished).
		Distinct().
		Order("repair_team").
		Pluck("repair_team", &values).Error
	if err != nil {
		return nil, fmt.Errorf("查询结算班组选项失败: %w", err)
	}
	return values, nil
}

// DistinctPeriods 返回已完工维修记录覆盖的月份(YYYY-MM), 倒序。
// 日期函数在 sqlite/postgres 不一致, 因此取回完工时间在应用层去重。
func (r *Repository) DistinctPeriods(ctx context.Context) ([]string, error) {
	times := make([]time.Time, 0)
	err := r.session(ctx).Model(&repair.Repair{}).
		Where("status = ? AND finished_at IS NOT NULL", repair.StatusFinished).
		Order("finished_at DESC").
		Pluck("finished_at", &times).Error
	if err != nil {
		return nil, fmt.Errorf("查询结算月份选项失败: %w", err)
	}
	seen := map[string]bool{}
	periods := make([]string, 0)
	for _, item := range times {
		period := item.Format("2006-01")
		if !seen[period] {
			seen[period] = true
			periods = append(periods, period)
		}
	}
	return periods, nil
}

// parsePeriod 解析 YYYY-MM 月份, 返回当月第一天零点。
func parsePeriod(value string) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01", strings.TrimSpace(value), time.Local)
	if err != nil {
		return time.Time{}, errInvalidPeriod(value)
	}
	return parsed, nil
}
