package settlement

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/repair"
	"streetlight/pkg/pagination"
)

// settlementSortSpec 定义结算单列表允许的排序字段白名单。
var settlementSortSpec = pagination.SortSpec{
	Allowed: map[string]string{
		"settle_no":    "settle_no",
		"repair_team":  "repair_team",
		"period":       "period",
		"status":       "status",
		"version":      "version",
		"total_amount": "total_amount",
		"created_at":   "created_at",
		"updated_at":   "updated_at",
	},
	Default: "period",
}

// Service 承载维修费用归集与结算流转的业务规则。
type Service struct {
	repo *Repository
	db   *gorm.DB
}

// NewService 构造结算服务。
func NewService(repo *Repository, db *gorm.DB) *Service {
	return &Service{repo: repo, db: db}
}

// Create 按班组与完工月份归集已完工维修费用并建账(草稿), 同时固化第 1 版明细快照。
func (s *Service) Create(ctx context.Context, req CreateRequest) (*Detail, error) {
	team := strings.TrimSpace(req.RepairTeam)
	if team == "" {
		return nil, apperr.BadRequest("维修班组不能为空")
	}
	period := strings.TrimSpace(req.Period)
	if _, err := parsePeriod(period); err != nil {
		return nil, err
	}

	existing, err := s.repo.GetByTeamPeriod(ctx, team, period)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperr.Conflict("班组 %s 的 %s 月份结算单 %s 已存在, 不能重复建账", team, period, existing.SettleNo)
	}

	records, err := s.repo.CollectFinishedRepairs(ctx, team, period)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, apperr.BadRequest("班组 %s 在 %s 月份没有已完工的维修记录, 无法建账", team, period)
	}

	now := time.Now()
	entity := &Settlement{
		RepairTeam:  team,
		Period:      period,
		Status:      StatusDraft,
		Version:     1,
		RecordCount: len(records),
		TotalAmount: round2(sumCost(records)),
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := NewRepository(tx)
		if err := txRepo.CreateWithUniqueNo(ctx, entity, "JS"+strings.ReplaceAll(period, "-", "")); err != nil {
			return err
		}
		if err := txRepo.CreateItems(ctx, buildItems(entity.ID, 1, records, now)); err != nil {
			return err
		}
		return txRepo.CreateFlow(ctx, &SettlementFlow{
			SettlementID: entity.ID,
			Version:      1,
			Action:       ActionCreate,
			Reason:       fmt.Sprintf("按班组与完工月份建账, 归集 %d 条维修记录", len(records)),
			RecordCount:  len(records),
			TotalAmount:  entity.TotalAmount,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, entity.ID)
}

// Submit 提交草稿结算单进入审核。
func (s *Service) Submit(ctx context.Context, id uint, req SubmitRequest) (*Detail, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status != StatusDraft {
		return nil, invalidTransition(entity, StatusSubmitted)
	}

	now := time.Now()
	entity.Status = StatusSubmitted
	entity.SubmittedAt = &now
	entity.LastReason = strings.TrimSpace(req.Reason)
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	if err := s.repo.CreateFlow(ctx, &SettlementFlow{
		SettlementID: entity.ID,
		Version:      entity.Version,
		Action:       ActionSubmit,
		Reason:       strings.TrimSpace(req.Reason),
		Operator:     strings.TrimSpace(req.Operator),
		RecordCount:  entity.RecordCount,
		TotalAmount:  entity.TotalAmount,
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Reject 驳回待审核结算单, 必须填写驳回原因, 驳回后对应月份解锁可修改维修记录。
func (s *Service) Reject(ctx context.Context, id uint, req AuditRequest) (*Detail, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status != StatusSubmitted {
		return nil, invalidTransition(entity, StatusRejected)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, apperr.BadRequest("驳回时必须填写驳回原因")
	}

	entity.Status = StatusRejected
	entity.LastReason = reason
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	if err := s.repo.CreateFlow(ctx, &SettlementFlow{
		SettlementID: entity.ID,
		Version:      entity.Version,
		Action:       ActionReject,
		Reason:       reason,
		Operator:     strings.TrimSpace(req.Operator),
		RecordCount:  entity.RecordCount,
		TotalAmount:  entity.TotalAmount,
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Resubmit 驳回后重新提交: 重新归集当前维修费用生成新版本快照, 保留历史版本用于差异对比。
func (s *Service) Resubmit(ctx context.Context, id uint, req SubmitRequest) (*Detail, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status != StatusRejected {
		return nil, invalidTransition(entity, StatusSubmitted)
	}

	records, err := s.repo.CollectFinishedRepairs(ctx, entity.RepairTeam, entity.Period)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, apperr.BadRequest("班组 %s 在 %s 月份已没有已完工维修记录, 不能重新提交", entity.RepairTeam, entity.Period)
	}

	newVersion := entity.Version + 1
	now := time.Now()
	amount := round2(sumCost(records))
	reason := strings.TrimSpace(req.Reason)

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := NewRepository(tx)
		if err := txRepo.CreateItems(ctx, buildItems(entity.ID, newVersion, records, now)); err != nil {
			return err
		}
		entity.Version = newVersion
		entity.RecordCount = len(records)
		entity.TotalAmount = amount
		entity.Status = StatusSubmitted
		submittedAt := now
		entity.SubmittedAt = &submittedAt
		entity.LastReason = reason
		if err := txRepo.Update(ctx, entity); err != nil {
			return err
		}
		return txRepo.CreateFlow(ctx, &SettlementFlow{
			SettlementID: entity.ID,
			Version:      newVersion,
			Action:       ActionResubmit,
			Reason:       reason,
			Operator:     strings.TrimSpace(req.Operator),
			RecordCount:  len(records),
			TotalAmount:  amount,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Approve 审核通过, 对应班组月份的维修记录从此锁定。
func (s *Service) Approve(ctx context.Context, id uint, req AuditRequest) (*Detail, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status != StatusSubmitted {
		return nil, invalidTransition(entity, StatusApproved)
	}

	now := time.Now()
	entity.Status = StatusApproved
	entity.ApprovedAt = &now
	entity.LastReason = strings.TrimSpace(req.Reason)
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	if err := s.repo.CreateFlow(ctx, &SettlementFlow{
		SettlementID: entity.ID,
		Version:      entity.Version,
		Action:       ActionApprove,
		Reason:       strings.TrimSpace(req.Reason),
		Operator:     strings.TrimSpace(req.Operator),
		RecordCount:  entity.RecordCount,
		TotalAmount:  entity.TotalAmount,
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete 删除草稿或已驳回的结算单及其快照与流水。
func (s *Service) Delete(ctx context.Context, id uint) error {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if entity.Status == StatusSubmitted || entity.Status == StatusApproved {
		return apperr.Conflict("结算单 %s 当前为%s状态, 不允许删除", entity.SettleNo, StatusLabel(entity.Status))
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("settlement_id = ?", id).Delete(&SettlementItem{}).Error; err != nil {
			return fmt.Errorf("删除结算明细失败: %w", err)
		}
		if err := tx.Where("settlement_id = ?", id).Delete(&SettlementFlow{}).Error; err != nil {
			return fmt.Errorf("删除流转流水失败: %w", err)
		}
		if err := tx.Delete(&Settlement{}, id).Error; err != nil {
			return fmt.Errorf("删除结算单失败: %w", err)
		}
		return nil
	})
}

// Get 查询结算单详情, 含最新版本明细、流转流水以及与实时归集数据的差异标记。
func (s *Service) Get(ctx context.Context, id uint) (*Detail, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListItems(ctx, id, entity.Version)
	if err != nil {
		return nil, err
	}
	flows, err := s.repo.ListFlows(ctx, id)
	if err != nil {
		return nil, err
	}

	detail := &Detail{Settlement: entity, Items: items, Flows: flows}
	live, err := s.repo.CollectFinishedRepairs(ctx, entity.RepairTeam, entity.Period)
	if err != nil {
		return nil, err
	}
	detail.CurrentCount = len(live)
	detail.CurrentAmount = round2(sumCost(live))
	detail.Stale = detail.CurrentCount != entity.RecordCount ||
		math.Abs(detail.CurrentAmount-entity.TotalAmount) > 0.001
	return detail, nil
}

// List 分页查询结算单。
func (s *Service) List(ctx context.Context, query ListQuery) ([]Settlement, int64, pagination.Query, error) {
	page := pagination.Parse(query.Params, settlementSortSpec)
	status := strings.TrimSpace(query.Status)
	if status != "" && !IsValidStatus(status) {
		return nil, 0, page, apperr.BadRequest("非法的结算单状态: %s", status)
	}
	period := strings.TrimSpace(query.Period)
	if period != "" {
		if _, err := parsePeriod(period); err != nil {
			return nil, 0, page, err
		}
	}
	filter := Filter{
		Keyword: strings.TrimSpace(query.Keyword),
		Status:  status,
		Team:    strings.TrimSpace(query.Team),
		Period:  period,
	}
	items, total, err := s.repo.List(ctx, filter, page)
	if err != nil {
		return nil, 0, page, err
	}
	return items, total, page, nil
}

// Diff 对比同一结算单两个版本的明细与金额差异。
func (s *Service) Diff(ctx context.Context, id uint, fromVersion, toVersion int) (*VersionDiff, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if fromVersion < 1 || toVersion < 1 || fromVersion > entity.Version || toVersion > entity.Version {
		return nil, apperr.BadRequest("版本号超出范围, 当前结算单共 %d 版", entity.Version)
	}
	fromItems, err := s.repo.ListItems(ctx, id, fromVersion)
	if err != nil {
		return nil, err
	}
	toItems, err := s.repo.ListItems(ctx, id, toVersion)
	if err != nil {
		return nil, err
	}
	return buildVersionDiff(fromVersion, toVersion, fromItems, toItems), nil
}

// Preview 建账前预览指定班组月份的实时归集结果, 不落库; 若已存在结算单则一并提示。
func (s *Service) Preview(ctx context.Context, team, period string) (*PreviewResult, error) {
	team = strings.TrimSpace(team)
	if team == "" {
		return nil, apperr.BadRequest("维修班组不能为空")
	}
	period = strings.TrimSpace(period)
	if _, err := parsePeriod(period); err != nil {
		return nil, err
	}
	records, err := s.repo.CollectFinishedRepairs(ctx, team, period)
	if err != nil {
		return nil, err
	}
	result := &PreviewResult{
		RepairTeam:  team,
		Period:      period,
		RecordCount: len(records),
		TotalAmount: round2(sumCost(records)),
		Records:     records,
	}
	if existing, err := s.repo.GetByTeamPeriod(ctx, team, period); err != nil {
		return nil, err
	} else if existing != nil {
		result.ExistingSettleNo = existing.SettleNo
		result.ExistingStatus = existing.Status
	}
	return result, nil
}

// Metadata 返回结算模块字典与可建账的班组、月份。
func (s *Service) Metadata(ctx context.Context) (*Meta, error) {
	teams, err := s.repo.DistinctTeams(ctx)
	if err != nil {
		return nil, err
	}
	periods, err := s.repo.DistinctPeriods(ctx)
	if err != nil {
		return nil, err
	}
	return &Meta{Statuses: Statuses(), Actions: Actions(), Teams: teams, Periods: periods}, nil
}

// Ledger 分页查询费用台账(已完工维修记录), 附带每条记录所属班组月份的结算状态。
func (s *Service) Ledger(ctx context.Context, query RepairRecordQuery) ([]RepairRecord, int64, pagination.Query, *LedgerSummary, error) {
	page := pagination.Parse(query.Params, ledgerSortSpec)
	filter, err := s.buildLedgerFilter(query)
	if err != nil {
		return nil, 0, page, nil, err
	}

	approvedKeys, err := s.repo.ApprovedTeamPeriods(ctx)
	if err != nil {
		return nil, 0, page, nil, err
	}
	filter.ApprovedKeys = approvedKeys

	rows, total, err := s.repo.LedgerList(ctx, filter, page)
	if err != nil {
		return nil, 0, page, nil, err
	}

	// 本页记录涉及的班组月份, 批量取对应结算单(含待审核/已驳回等中间状态)。
	keys := make([]string, 0, len(rows))
	for _, item := range rows {
		if item.FinishedAt != nil && strings.TrimSpace(item.RepairTeam) != "" {
			keys = append(keys, teamPeriodKey(item.RepairTeam, item.FinishedAt.Format("2006-01")))
		}
	}
	settlementMap, err := s.repo.settlementMapByTeamPeriod(ctx, keys)
	if err != nil {
		return nil, 0, page, nil, err
	}

	records := make([]RepairRecord, 0, len(rows))
	for _, item := range rows {
		records = append(records, s.toLedgerRecord(item, settlementMap, approvedKeys))
	}

	summary, err := s.buildLedgerSummary(ctx, filter, approvedKeys)
	if err != nil {
		return nil, 0, page, nil, err
	}
	return records, total, page, summary, nil
}

// EnsureRepairMutable 实现维修模块的锁定端口:
// 班组在某完工月份存在待审核或已通过的结算单时, 该月维修记录不允许改动(完工/修改/删除)。
func (s *Service) EnsureRepairMutable(ctx context.Context, team string, finishedAt time.Time) error {
	team = strings.TrimSpace(team)
	if team == "" {
		return nil
	}
	existing, err := s.repo.GetByTeamPeriod(ctx, team, finishedAt.Format("2006-01"))
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	if existing.Status == StatusSubmitted || existing.Status == StatusApproved {
		return apperr.Conflict(
			"班组 %s 的 %s 月份维修费用已提交结算(结算单 %s, %s), 该月份维修记录不允许改动",
			team, existing.Period, existing.SettleNo, StatusLabel(existing.Status),
		)
	}
	return nil
}

// buildLedgerFilter 解析台账查询条件与完工时间区间。
func (s *Service) buildLedgerFilter(query RepairRecordQuery) (LedgerFilter, error) {
	period := strings.TrimSpace(query.Period)
	var from, to *time.Time
	if period != "" {
		start, err := parsePeriod(period)
		if err != nil {
			return LedgerFilter{}, err
		}
		from, to = &start, nil
		monthEnd := start.AddDate(0, 1, 0)
		to = &monthEnd
	}
	if value := strings.TrimSpace(query.StartDate); value != "" {
		start, err := parseDay(value)
		if err != nil {
			return LedgerFilter{}, err
		}
		from = &start
	}
	if value := strings.TrimSpace(query.EndDate); value != "" {
		end, err := parseDay(value)
		if err != nil {
			return LedgerFilter{}, err
		}
		next := end.AddDate(0, 0, 1)
		to = &next
	}
	if from != nil && to != nil && to.Before(*from) {
		return LedgerFilter{}, apperr.BadRequest("结束日期不能早于开始日期")
	}

	filter := LedgerFilter{
		Keyword:    strings.TrimSpace(query.Keyword),
		RepairTeam: strings.TrimSpace(query.RepairTeam),
		From:       from,
		To:         to,
	}
	switch strings.TrimSpace(query.Settled) {
	case "":
	case "yes":
		filter.SettledOnly = true
	case "no":
		filter.PendingOnly = true
	default:
		return filter, apperr.BadRequest("settled 只允许取 yes 或 no")
	}
	return filter, nil
}

// buildLedgerSummary 汇总台账(受当前班组/关键字/区间条件约束, 不受分页影响)的已结算与待结算金额。
func (s *Service) buildLedgerSummary(ctx context.Context, baseFilter LedgerFilter, approvedKeys map[string]bool) (*LedgerSummary, error) {
	summary := &LedgerSummary{}

	totalCount, totalAmount, err := s.repo.LedgerTotals(ctx, baseFilter)
	if err != nil {
		return nil, err
	}
	summary.TotalCount = totalCount
	summary.TotalAmount = round2(totalAmount)

	settledFilter := baseFilter
	settledFilter.SettledOnly = true
	settledFilter.PendingOnly = false
	settledFilter.ApprovedKeys = approvedKeys
	settledCount, settledAmount, err := s.repo.LedgerTotals(ctx, settledFilter)
	if err != nil {
		return nil, err
	}
	summary.SettledCount = settledCount
	summary.SettledAmount = round2(settledAmount)

	pendingFilter := baseFilter
	pendingFilter.SettledOnly = false
	pendingFilter.PendingOnly = true
	pendingFilter.ApprovedKeys = approvedKeys
	pendingCount, pendingAmount, err := s.repo.LedgerTotals(ctx, pendingFilter)
	if err != nil {
		return nil, err
	}
	summary.PendingCount = pendingCount
	summary.PendingAmount = round2(pendingAmount)
	return summary, nil
}

// toLedgerRecord 将维修记录转换为台账行并标注结算状态。
func (s *Service) toLedgerRecord(item repair.Repair, settlementMap map[string]Settlement, approvedKeys map[string]bool) RepairRecord {
	period := ""
	finishedAt := ""
	if item.FinishedAt != nil {
		period = item.FinishedAt.Format("2006-01")
		finishedAt = item.FinishedAt.Format("2006-01-02 15:04:05")
	}
	row := RepairRecord{
		ID:         item.ID,
		RepairNo:   item.RepairNo,
		FaultNo:    item.FaultNo,
		LampCode:   item.LampCode,
		Repairman:  item.Repairman,
		RepairTeam: item.RepairTeam,
		FinishedAt: finishedAt,
		Period:     period,
		Cost:       item.Cost,
		Content:    item.Content,
		Materials:  item.Materials,
	}
	key := teamPeriodKey(item.RepairTeam, period)
	if entity, ok := settlementMap[key]; ok {
		row.SettlementID = &entity.ID
		row.SettleNo = entity.SettleNo
		row.SettlementStatus = entity.Status
	}
	row.Locked = approvedKeys[key] || row.SettlementStatus == StatusSubmitted
	return row
}

// invalidTransition 构造状态流转冲突错误。
func invalidTransition(entity *Settlement, target string) error {
	return apperr.Conflict(
		"结算单 %s 当前状态为「%s」, 不允许执行该操作(目标状态: %s)",
		entity.SettleNo, StatusLabel(entity.Status), StatusLabel(target),
	)
}

// buildItems 将归集到的维修记录转换为指定版本的明细快照。
func buildItems(settlementID uint, version int, records []repair.Repair, now time.Time) []SettlementItem {
	items := make([]SettlementItem, 0, len(records))
	for _, record := range records {
		finishedAt := now
		if record.FinishedAt != nil {
			finishedAt = *record.FinishedAt
		}
		items = append(items, SettlementItem{
			SettlementID: settlementID,
			Version:      version,
			RepairID:     record.ID,
			RepairNo:     record.RepairNo,
			FaultNo:      record.FaultNo,
			LampCode:     record.LampCode,
			Repairman:    record.Repairman,
			FinishedAt:   finishedAt,
			Cost:         record.Cost,
			Content:      record.Content,
			Materials:    record.Materials,
			CreatedAt:    now,
		})
	}
	return items
}

// sumCost 汇总维修记录费用。
func sumCost(records []repair.Repair) float64 {
	var total float64
	for _, item := range records {
		total += item.Cost
	}
	return total
}

// parseDay 解析 YYYY-MM-DD 日期, 返回当天零点。
func parseDay(value string) (time.Time, error) {
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), time.Local)
	if err != nil {
		return time.Time{}, apperr.BadRequest("日期格式应为 YYYY-MM-DD, 当前值: %s", value)
	}
	return date, nil
}
