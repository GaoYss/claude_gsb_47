package settlement_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/repair"
	"streetlight/internal/modules/settlement"
)

type harness struct {
	db      *gorm.DB
	svc     *settlement.Service
	repo    *settlement.Repository
	counter int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&repair.Repair{}, &settlement.Settlement{}, &settlement.SettlementItem{}, &settlement.SettlementFlow{}))

	repo := settlement.NewRepository(db)
	return &harness{db: db, repo: repo, svc: settlement.NewService(repo, db)}
}

func (h *harness) addRepair(t *testing.T, team string, finished time.Time, cost float64) repair.Repair {
	t.Helper()
	h.counter++
	entity := repair.Repair{
		RepairNo:   "WX" + finished.Format("20060102") + "T" + itoa(h.counter),
		FaultID:    uint(h.counter),
		FaultNo:    "GD" + finished.Format("20060102") + "T" + itoa(h.counter),
		LampCode:   "LD-T",
		Repairman:  "维修工",
		RepairTeam: team,
		StartedAt:  finished.Add(-2 * time.Hour),
		FinishedAt: &finished,
		Status:     repair.StatusFinished,
		Result:     repair.ResultFixed,
		Content:    "测试维修",
		Materials:  "材料",
		Cost:       cost,
	}
	require.NoError(t, h.db.Create(&entity).Error)
	return entity
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func date(day int, hour int) time.Time {
	return time.Date(2026, 3, day, hour, 0, 0, 0, time.Local)
}

func requireStatus(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	businessErr, ok := apperr.As(err)
	require.True(t, ok, "期望业务错误, 实际: %v", err)
	require.Equal(t, status, businessErr.Status, businessErr.Message)
}

func TestCreateAggregatesRepairCosts(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.addRepair(t, "一班", date(5, 10), 100)
	h.addRepair(t, "一班", date(20, 14), 230.5)
	h.addRepair(t, "一班", date(20, 16), 19.5)
	h.addRepair(t, "二班", date(20, 14), 500)                 // 其它班组
	h.addRepair(t, "一班", date(1, 9).AddDate(0, -1, 0), 999) // 上月(2 月), 不应归集

	detail, err := h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-03"})
	require.NoError(t, err)
	require.Equal(t, settlement.StatusDraft, detail.Status)
	require.Equal(t, 1, detail.Version)
	require.Equal(t, 3, detail.RecordCount, "只归集 3 月一班的 3 条记录")
	require.InDelta(t, 350.0, detail.TotalAmount, 0.001, "结算金额必须等于维修记录费用合计")
	require.Len(t, detail.Items, 3)
	require.NotEmpty(t, detail.SettleNo)
	require.Contains(t, detail.SettleNo, "JS202603")
	require.False(t, detail.Stale)
}

func TestCreateValidation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	// 月份格式错误
	_, err := h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-3"})
	requireStatus(t, err, http.StatusBadRequest)

	// 该班组月份没有已完工记录
	_, err = h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-03"})
	requireStatus(t, err, http.StatusBadRequest)

	h.addRepair(t, "一班", date(5, 10), 100)
	_, err = h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-03"})
	require.NoError(t, err)

	// 同一班组月份不允许重复建账
	_, err = h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-03"})
	requireStatus(t, err, http.StatusConflict)
}

func TestSubmitRejectResubmitAndDiff(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	r1 := h.addRepair(t, "一班", date(5, 10), 100)
	r2 := h.addRepair(t, "一班", date(6, 10), 200)

	created, err := h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-03"})
	require.NoError(t, err)
	require.Equal(t, 300.0, created.TotalAmount)

	submitted, err := h.svc.Submit(ctx, created.ID, settlement.SubmitRequest{Operator: "张三", Reason: "申请结算"})
	require.NoError(t, err)
	require.Equal(t, settlement.StatusSubmitted, submitted.Status)

	// 待审核期间不允许重复提交
	_, err = h.svc.Submit(ctx, created.ID, settlement.SubmitRequest{})
	requireStatus(t, err, http.StatusConflict)

	// 驳回必须填写原因
	_, err = h.svc.Reject(ctx, created.ID, settlement.AuditRequest{Operator: "财务"})
	requireStatus(t, err, http.StatusBadRequest)

	rejected, err := h.svc.Reject(ctx, created.ID, settlement.AuditRequest{Operator: "财务", Reason: "金额有误"})
	require.NoError(t, err)
	require.Equal(t, settlement.StatusRejected, rejected.Status)
	require.Equal(t, "金额有误", rejected.LastReason)

	// 驳回后: 修改 r1 费用, 再新增一条维修单, 然后重新归集提交
	require.NoError(t, h.db.Model(&repair.Repair{}).Where("id = ?", r1.ID).Update("cost", 150).Error)
	r3 := h.addRepair(t, "一班", date(8, 10), 50)
	_ = r3
	// 删除 r2, 模拟多退少补
	require.NoError(t, h.db.Delete(&repair.Repair{}, r2.ID).Error)

	resubmitted, err := h.svc.Resubmit(ctx, created.ID, settlement.SubmitRequest{Operator: "张三", Reason: "已按驳回意见调整"})
	require.NoError(t, err)
	require.Equal(t, settlement.StatusSubmitted, resubmitted.Status)
	require.Equal(t, 2, resubmitted.Version)
	require.Equal(t, 2, resubmitted.RecordCount) // r1(改) + r3(新)
	require.InDelta(t, 200.0, resubmitted.TotalAmount, 0.001)

	// 历史版本 v1 快照仍然保留
	oldItems, err := h.repo.ListItems(ctx, created.ID, 1)
	require.NoError(t, err)
	require.Len(t, oldItems, 2, "驳回重提不应覆盖历史版本快照")

	diff, err := h.svc.Diff(ctx, created.ID, 1, 2)
	require.NoError(t, err)
	require.Equal(t, 2, diff.FromCount)
	require.Equal(t, 300.0, diff.FromAmount)
	require.Equal(t, 2, diff.ToCount)
	require.InDelta(t, 200.0, diff.ToAmount, 0.001)
	require.InDelta(t, -100.0, diff.DeltaAmount, 0.001)

	byType := map[string]int{}
	for _, item := range diff.Items {
		byType[item.Type]++
		switch item.Type {
		case "added":
			require.InDelta(t, 50.0, item.Delta, 0.001)
		case "removed":
			require.InDelta(t, -200.0, item.Delta, 0.001)
		case "changed":
			require.InDelta(t, 50.0, item.Delta, 0.001)
			require.Equal(t, 100.0, item.FromCost)
			require.Equal(t, 150.0, item.ToCost)
		}
	}
	require.Equal(t, 1, byType["added"])
	require.Equal(t, 1, byType["removed"])
	require.Equal(t, 1, byType["changed"])

	// 流转流水完整保留每次原因(建账/提交/驳回/重提)
	flows, err := h.repo.ListFlows(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, flows, 4)
	actions := []string{}
	for _, f := range flows {
		actions = append(actions, f.Action)
	}
	require.Equal(t,
		[]string{"create", "submit", "reject", "resubmit"},
		actions)

	// 审核通过
	approved, err := h.svc.Approve(ctx, created.ID, settlement.AuditRequest{Operator: "财务", Reason: "复核无误"})
	require.NoError(t, err)
	require.Equal(t, settlement.StatusApproved, approved.Status)
	require.NotNil(t, approved.ApprovedAt)

	// 通过后不允许再驳回/重提/提交
	_, err = h.svc.Reject(ctx, created.ID, settlement.AuditRequest{Reason: "x"})
	requireStatus(t, err, http.StatusConflict)
	_, err = h.svc.Resubmit(ctx, created.ID, settlement.SubmitRequest{})
	requireStatus(t, err, http.StatusConflict)
}

func TestApprovedAndSubmittedPeriodLocksRepairs(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.addRepair(t, "一班", date(5, 10), 100)
	h.addRepair(t, "二班", date(5, 10), 300)

	one, err := h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-03"})
	require.NoError(t, err)
	_, err = h.svc.Submit(ctx, one.ID, settlement.SubmitRequest{})
	require.NoError(t, err)

	// 待审核期间已锁定
	err = h.svc.EnsureRepairMutable(ctx, "一班", date(10, 8))
	requireStatus(t, err, http.StatusConflict)
	// 其它班组不受影响
	require.NoError(t, h.svc.EnsureRepairMutable(ctx, "二班", date(10, 8)))
	// 其它月份不受影响
	require.NoError(t, h.svc.EnsureRepairMutable(ctx, "一班", date(1, 8).AddDate(0, 1, 0)))

	// 驳回后解锁, 可调整维修记录
	_, err = h.svc.Reject(ctx, one.ID, settlement.AuditRequest{Reason: "需要调整"})
	require.NoError(t, err)
	require.NoError(t, h.svc.EnsureRepairMutable(ctx, "一班", date(10, 8)))

	// 重新提交并通过后再次锁定
	_, err = h.svc.Resubmit(ctx, one.ID, settlement.SubmitRequest{})
	require.NoError(t, err)
	_, err = h.svc.Approve(ctx, one.ID, settlement.AuditRequest{Reason: "ok"})
	require.NoError(t, err)
	err = h.svc.EnsureRepairMutable(ctx, "一班", date(10, 8))
	requireStatus(t, err, http.StatusConflict)

	// 空班组不校验(历史维修单可能未填班组)
	require.NoError(t, h.svc.EnsureRepairMutable(ctx, "", date(10, 8)))
}

func TestLedgerMarksSettlementStatusAndSummary(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.addRepair(t, "一班", date(5, 10), 100)
	h.addRepair(t, "一班", date(6, 10), 200)
	h.addRepair(t, "二班", date(6, 10), 300)

	one, err := h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-03"})
	require.NoError(t, err)
	_, err = h.svc.Submit(ctx, one.ID, settlement.SubmitRequest{})
	require.NoError(t, err)
	_, err = h.svc.Approve(ctx, one.ID, settlement.AuditRequest{Reason: "ok"})
	require.NoError(t, err)

	records, total, _, summary, err := h.svc.Ledger(ctx, settlement.RepairRecordQuery{Period: "2026-03"})
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, records, 3)
	require.Equal(t, int64(3), summary.TotalCount)
	require.InDelta(t, 600.0, summary.TotalAmount, 0.001)
	require.Equal(t, int64(2), summary.SettledCount)
	require.InDelta(t, 300.0, summary.SettledAmount, 0.001)
	require.Equal(t, int64(1), summary.PendingCount)
	require.InDelta(t, 300.0, summary.PendingAmount, 0.001)

	locked, pending := 0, 0
	for _, row := range records {
		if row.RepairTeam == "一班" {
			require.True(t, row.Locked)
			require.Equal(t, settlement.StatusApproved, row.SettlementStatus)
			require.NotEmpty(t, row.SettleNo)
			locked++
		} else {
			require.False(t, row.Locked)
			require.Empty(t, row.SettlementStatus)
			pending++
		}
	}
	require.Equal(t, 2, locked)
	require.Equal(t, 1, pending)

	// settled=no 只返回待结算
	_, pendingTotal, _, pendingSummary, err := h.svc.Ledger(ctx, settlement.RepairRecordQuery{Settled: "no"})
	require.NoError(t, err)
	require.Equal(t, int64(1), pendingTotal)
	require.Equal(t, int64(1), pendingSummary.PendingCount)
}

func TestDeleteSettlementRules(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.addRepair(t, "一班", date(5, 10), 100)
	entity, err := h.svc.Create(ctx, settlement.CreateRequest{RepairTeam: "一班", Period: "2026-03"})
	require.NoError(t, err)
	_, err = h.svc.Submit(ctx, entity.ID, settlement.SubmitRequest{})
	require.NoError(t, err)

	// 待审核不允许删除
	requireStatus(t, h.svc.Delete(ctx, entity.ID), http.StatusConflict)

	_, err = h.svc.Reject(ctx, entity.ID, settlement.AuditRequest{Reason: "退回"})
	require.NoError(t, err)
	// 已驳回可以删除, 快照与流水一并清除
	require.NoError(t, h.svc.Delete(ctx, entity.ID))
	_, err = h.svc.Get(ctx, entity.ID)
	requireStatus(t, err, http.StatusNotFound)
	items, err := h.repo.ListItems(ctx, entity.ID, 1)
	require.NoError(t, err)
	require.Empty(t, items)
}
