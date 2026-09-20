package settlement

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"streetlight/internal/apperr"
	"streetlight/pkg/pagination"
)

// Filter 结算单查询条件。
type Filter struct {
	Keyword string
	Status  string
	Team    string
	Period  string
}

// Repository 负责结算单及其明细快照、流转流水的数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造结算仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) session(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

// Create 保存结算单。
func (r *Repository) Create(ctx context.Context, entity *Settlement) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("创建结算单失败: %w", err)
	}
	return nil
}

// CreateWithUniqueNo 生成唯一结算单号并落库, 冲突时自动重试。
func (r *Repository) CreateWithUniqueNo(ctx context.Context, entity *Settlement, prefix string) error {
	for attempt := 0; attempt < 5; attempt++ {
		sequence, err := r.NextSequence(ctx, prefix)
		if err != nil {
			return err
		}
		entity.SettleNo = fmt.Sprintf("%s%04d", prefix, sequence+attempt)
		err = r.Create(ctx, entity)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err) || !strings.Contains(err.Error(), "settle_no") {
			// 班组+月份唯一冲突与单号冲突需要区别处理, 这里交由上层兜底信息。
			if isUniqueViolation(err) {
				return apperr.Conflict("班组 %s 的 %s 月份结算单已存在, 不能重复建账", entity.RepairTeam, entity.Period)
			}
			return err
		}
	}
	return apperr.Conflict("结算单号生成冲突, 请稍后重试")
}

// NextSequence 返回指定前缀下可用的下一个流水号。
func (r *Repository) NextSequence(ctx context.Context, prefix string) (int, error) {
	var latest string
	err := r.session(ctx).Model(&Settlement{}).
		Where("settle_no LIKE ?", prefix+"%").
		Order("settle_no DESC").
		Limit(1).
		Pluck("settle_no", &latest).Error
	if err != nil {
		return 0, fmt.Errorf("生成结算单号失败: %w", err)
	}
	if latest == "" {
		return 1, nil
	}
	value, convErr := strconv.Atoi(strings.TrimPrefix(latest, prefix))
	if convErr != nil {
		return 1, nil
	}
	return value + 1, nil
}

// Update 保存结算单全部字段。
func (r *Repository) Update(ctx context.Context, entity *Settlement) error {
	if err := r.session(ctx).Save(entity).Error; err != nil {
		return fmt.Errorf("更新结算单失败: %w", err)
	}
	return nil
}

// GetByID 按主键查询结算单。
func (r *Repository) GetByID(ctx context.Context, id uint) (*Settlement, error) {
	var entity Settlement
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("结算单不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询结算单失败: %w", err)
	}
	return &entity, nil
}

// GetByTeamPeriod 按班组与月份查询结算单, 不存在时返回 nil。
func (r *Repository) GetByTeamPeriod(ctx context.Context, team, period string) (*Settlement, error) {
	var entity Settlement
	err := r.session(ctx).Where("repair_team = ? AND period = ?", team, period).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询班组月份结算单失败: %w", err)
	}
	return &entity, nil
}

// List 分页查询结算单。
func (r *Repository) List(ctx context.Context, filter Filter, page pagination.Query) ([]Settlement, int64, error) {
	base := func() *gorm.DB {
		statement := r.session(ctx).Model(&Settlement{})
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			statement = statement.Where("settle_no LIKE ? OR repair_team LIKE ?", like, like)
		}
		if filter.Status != "" {
			statement = statement.Where("status = ?", filter.Status)
		}
		if value := strings.TrimSpace(filter.Team); value != "" {
			statement = statement.Where("repair_team = ?", value)
		}
		if value := strings.TrimSpace(filter.Period); value != "" {
			statement = statement.Where("period = ?", value)
		}
		return statement
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计结算单失败: %w", err)
	}

	entities := make([]Settlement, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("查询结算单失败: %w", err)
	}
	return entities, total, nil
}

// CreateItems 批量写入某一版本的明细快照。
func (r *Repository) CreateItems(ctx context.Context, items []SettlementItem) error {
	if len(items) == 0 {
		return nil
	}
	if err := r.session(ctx).Create(&items).Error; err != nil {
		return fmt.Errorf("写入结算明细失败: %w", err)
	}
	return nil
}

// ListItems 查询结算单指定版本的明细, 按完工时间正序。
func (r *Repository) ListItems(ctx context.Context, settlementID uint, version int) ([]SettlementItem, error) {
	items := make([]SettlementItem, 0)
	err := r.session(ctx).
		Where("settlement_id = ? AND version = ?", settlementID, version).
		Order("finished_at ASC, id ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("查询结算明细失败: %w", err)
	}
	return items, nil
}

// CreateFlow 写入一条流转流水。
func (r *Repository) CreateFlow(ctx context.Context, flow *SettlementFlow) error {
	if err := r.session(ctx).Create(flow).Error; err != nil {
		return fmt.Errorf("写入流转流水失败: %w", err)
	}
	return nil
}

// ListFlows 查询结算单全部流转流水, 按时间正序。
func (r *Repository) ListFlows(ctx context.Context, settlementID uint) ([]SettlementFlow, error) {
	flows := make([]SettlementFlow, 0)
	err := r.session(ctx).
		Where("settlement_id = ?", settlementID).
		Order("created_at ASC, id ASC").
		Find(&flows).Error
	if err != nil {
		return nil, fmt.Errorf("查询流转流水失败: %w", err)
	}
	return flows, nil
}

// ApprovedTeamPeriods 批量返回已审核通过的 (班组, 月份) 集合, 供台账标记锁定状态。
func (r *Repository) ApprovedTeamPeriods(ctx context.Context) (map[string]bool, error) {
	type row struct {
		RepairTeam string
		Period     string
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Settlement{}).
		Select("repair_team, period").
		Where("status = ?", StatusApproved).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("查询已结算月份失败: %w", err)
	}
	result := make(map[string]bool, len(rows))
	for _, item := range rows {
		result[teamPeriodKey(item.RepairTeam, item.Period)] = true
	}
	return result, nil
}

// IsTeamPeriodApproved 判断班组某月份是否已存在审核通过的结算单。
func (r *Repository) IsTeamPeriodApproved(ctx context.Context, team, period string) (bool, error) {
	var count int64
	err := r.session(ctx).Model(&Settlement{}).
		Where("repair_team = ? AND period = ? AND status = ?", team, period, StatusApproved).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("校验结算锁定状态失败: %w", err)
	}
	return count > 0, nil
}

// settlementMapByTeamPeriod 批量查询结算单并按 班组|月份 索引。
func (r *Repository) settlementMapByTeamPeriod(ctx context.Context, keys []string) (map[string]Settlement, error) {
	if len(keys) == 0 {
		return map[string]Settlement{}, nil
	}
	teams := make([]string, 0, len(keys))
	periods := make([]string, 0, len(keys))
	seenTeams := map[string]bool{}
	seenPeriods := map[string]bool{}
	for _, key := range keys {
		team, period, ok := splitTeamPeriodKey(key)
		if !ok {
			continue
		}
		if !seenTeams[team] {
			teams = append(teams, team)
			seenTeams[team] = true
		}
		if !seenPeriods[period] {
			periods = append(periods, period)
			seenPeriods[period] = true
		}
	}

	entities := make([]Settlement, 0)
	err := r.session(ctx).
		Where("repair_team IN ? AND period IN ?", teams, periods).
		Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("批量查询结算单失败: %w", err)
	}
	result := make(map[string]Settlement, len(entities))
	for _, item := range entities {
		result[teamPeriodKey(item.RepairTeam, item.Period)] = item
	}
	return result, nil
}

// isUniqueViolation 兼容 sqlite 与 postgres 的唯一约束冲突判断。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "unique violation")
}
