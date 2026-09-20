package repair_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/internal/modules/repair"
	"streetlight/internal/modules/settlement"
)

// harness 使用内存数据库装配真实模块, 用于验证跨模块业务流程。
type harness struct {
	lamps       *lamp.Service
	faults      *fault.Service
	repairs     *repair.Service
	settlements *settlement.Service
	db          *gorm.DB
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

	require.NoError(t, db.AutoMigrate(&lamp.Lamp{}, &fault.Fault{}, &repair.Repair{},
		&settlement.Settlement{}, &settlement.SettlementItem{}, &settlement.SettlementFlow{}))

	lampRepository := lamp.NewRepository(db)
	lampService := lamp.NewService(lampRepository)

	faultRepository := fault.NewRepository(db)
	faultService := fault.NewService(faultRepository, lampService)
	lampService.SetOpenFaultCounter(faultRepository)

	repairRepository := repair.NewRepository(db)
	repairService := repair.NewService(repairRepository, faultService)

	settlementRepository := settlement.NewRepository(db)
	settlementService := settlement.NewService(settlementRepository, db)
	repairService.SetSettlementLock(settlementService)

	return &harness{
		lamps: lampService, faults: faultService, repairs: repairService,
		settlements: settlementService, db: db,
	}
}

func (h *harness) createLamp(t *testing.T, code string) *lamp.Lamp {
	t.Helper()
	entity, err := h.lamps.Create(context.Background(), lamp.CreateRequest{
		Code:     code,
		Name:     "测试灯杆",
		RoadName: "测试路",
		LampType: lamp.LampTypeLED,
	})
	require.NoError(t, err)
	return entity
}

func (h *harness) createFault(t *testing.T, lampID uint, description string) *fault.Fault {
	t.Helper()
	entity, err := h.faults.Create(context.Background(), fault.CreateRequest{
		LampID:      lampID,
		FaultType:   "灯不亮",
		FaultLevel:  fault.LevelHigh,
		Source:      fault.SourceInspection,
		Description: description,
		Reporter:    "巡检员",
	})
	require.NoError(t, err)
	return entity
}

// requireConflict 断言错误是 409 业务冲突。
func requireConflict(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	businessErr, ok := apperr.As(err)
	require.True(t, ok, "期望业务错误, 实际: %v", err)
	require.Equal(t, http.StatusConflict, businessErr.Status, "错误信息: %s", businessErr.Message)
}

func TestFaultRepairLifecycle(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-001")

	entity := h.createFault(t, device.ID, "整灯不亮, 疑似驱动电源故障")
	require.Equal(t, fault.StatusPending, entity.Status)
	require.Regexp(t, `^GD\d{8}\d{4}$`, entity.FaultNo)

	afterReport, err := h.lamps.Get(ctx, device.ID)
	require.NoError(t, err)
	require.Equal(t, lamp.RunStatusFault, afterReport.RunStatus, "登记故障后路灯应变为故障状态")

	// 同一盏路灯不允许存在多条未闭环故障
	_, err = h.faults.Create(ctx, fault.CreateRequest{
		LampID: device.ID, FaultType: "灯不亮", Description: "重复登记",
	})
	requireConflict(t, err)

	// 维修开工
	record, err := h.repairs.Create(ctx, repair.CreateRequest{
		FaultID: entity.ID, Repairman: "维修工甲", RepairTeam: "市政照明一班",
	})
	require.NoError(t, err)
	require.Equal(t, repair.StatusOngoing, record.Status)
	require.Regexp(t, `^WX\d{8}\d{4}$`, record.RepairNo)

	faultAfterStart, err := h.faults.GetByID(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, fault.StatusProcessing, faultAfterStart.Status)
	require.Equal(t, 1, faultAfterStart.RepairCount)
	require.NotNil(t, faultAfterStart.LatestRepairID)
	require.Equal(t, record.ID, *faultAfterStart.LatestRepairID)

	lampAfterStart, err := h.lamps.Get(ctx, device.ID)
	require.NoError(t, err)
	require.Equal(t, lamp.RunStatusMaintenance, lampAfterStart.RunStatus)

	// 同一故障不允许并行开工
	_, err = h.repairs.Create(ctx, repair.CreateRequest{FaultID: entity.ID, Repairman: "维修工乙"})
	requireConflict(t, err)

	// 完工且结果为已修复
	cost := 210.0
	finished, err := h.repairs.Finish(ctx, record.ID, repair.FinishRequest{
		Result: repair.ResultFixed, Content: "更换驱动电源", Cost: &cost,
	})
	require.NoError(t, err)
	require.Equal(t, repair.StatusFinished, finished.Status)
	require.NotNil(t, finished.FinishedAt)

	faultAfterFinish, err := h.faults.GetByID(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, fault.StatusRepaired, faultAfterFinish.Status)

	lampAfterFinish, err := h.lamps.Get(ctx, device.ID)
	require.NoError(t, err)
	require.Equal(t, lamp.RunStatusNormal, lampAfterFinish.RunStatus, "修复后路灯应恢复为正常")

	// 关闭故障形成闭环
	closed, err := h.faults.Close(ctx, entity.ID, fault.CloseRequest{Remark: "现场复核通过"})
	require.NoError(t, err)
	require.Equal(t, fault.StatusClosed, closed.Status)
	require.NotNil(t, closed.ClosedAt)

	// 已产生的维修记录使故障不可删除
	requireConflict(t, h.faults.Delete(ctx, entity.ID))
	// 已关闭故障不允许再次关闭
	_, err = h.faults.Close(ctx, entity.ID, fault.CloseRequest{})
	requireConflict(t, err)
}

func TestRepairPendingPartsKeepsFaultProcessing(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-002")
	entity := h.createFault(t, device.ID, "线路老化需要更换电缆")

	record, err := h.repairs.Create(ctx, repair.CreateRequest{
		FaultID: entity.ID, Repairman: "维修工丙",
	})
	require.NoError(t, err)

	_, err = h.repairs.Finish(ctx, record.ID, repair.FinishRequest{Result: repair.ResultPendingParts})
	require.NoError(t, err)

	faultAfterFinish, err := h.faults.GetByID(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, fault.StatusProcessing, faultAfterFinish.Status, "非已修复结果不应结束故障")

	lampAfterFinish, err := h.lamps.Get(ctx, device.ID)
	require.NoError(t, err)
	require.Equal(t, lamp.RunStatusMaintenance, lampAfterFinish.RunStatus)

	// 可继续登记第二次维修(返修)
	second, err := h.repairs.Create(ctx, repair.CreateRequest{
		FaultID: entity.ID, Repairman: "维修工丙", Content: "物料到场后更换电缆",
	})
	require.NoError(t, err)
	require.Equal(t, repair.StatusOngoing, second.Status)

	faultAfterSecond, err := h.faults.GetByID(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, 2, faultAfterSecond.RepairCount)
}

func TestRepairRejectedOnClosedFault(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-003")
	entity := h.createFault(t, device.ID, "误报故障需要作废")

	_, err := h.faults.Close(ctx, entity.ID, fault.CloseRequest{Remark: "误报作废"})
	require.NoError(t, err)

	_, err = h.repairs.Create(ctx, repair.CreateRequest{FaultID: entity.ID, Repairman: "维修工丁"})
	requireConflict(t, err)
}

func TestFaultValidation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-004")

	// 非法故障类型
	_, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: device.ID, FaultType: "不存在的类型", Description: "测试",
	})
	businessErr, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, http.StatusBadRequest, businessErr.Status)

	// 路灯不存在
	_, err = h.faults.Create(ctx, fault.CreateRequest{
		LampID: 99999, FaultType: "灯不亮", Description: "测试",
	})
	businessErr, ok = apperr.As(err)
	require.True(t, ok)
	require.Equal(t, http.StatusNotFound, businessErr.Status)

	// 重复路灯编号
	_, err = h.lamps.Create(ctx, lamp.CreateRequest{
		Code: device.Code, RoadName: "测试路", LampType: lamp.LampTypeLED,
	})
	requireConflict(t, err)

	// 存在未闭环故障时不允许删除路灯
	h.createFault(t, device.ID, "删除校验")
	requireConflict(t, h.lamps.Delete(ctx, device.ID))
}

// TestRepairLockedBySettlement 验证已进入结算流程的班组月份, 其维修记录不能完工/删除。
func TestRepairLockedBySettlement(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	// 第一条: 完工后用于建账并审核通过, 已通过月份不允许删除
	deviceA := h.createLamp(t, "LD-T-101")
	faultA, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: deviceA.ID, FaultType: "灯不亮", Description: "需要结算锁定的已完工记录",
		ReportedAt: time.Date(2026, 3, 5, 8, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)
	finished, err := h.repairs.Create(ctx, repair.CreateRequest{
		FaultID: faultA.ID, Repairman: "维修工甲", RepairTeam: "结算班组",
		StartedAt: time.Date(2026, 3, 5, 9, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)
	cost := 120.0
	finished, err = h.repairs.Finish(ctx, finished.ID, repair.FinishRequest{
		Result:     repair.ResultFixed,
		FinishedAt: time.Date(2026, 3, 5, 11, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
		Cost:       &cost,
	})
	require.NoError(t, err)
	require.Equal(t, repair.StatusFinished, finished.Status)

	created, err := h.settlements.Create(ctx,
		settlement.CreateRequest{RepairTeam: "结算班组", Period: "2026-03"})
	require.NoError(t, err)
	require.Equal(t, 1, created.RecordCount)
	require.InDelta(t, 120.0, created.TotalAmount, 0.001)
	_, err = h.settlements.Submit(ctx, created.ID, settlement.SubmitRequest{Reason: "提交"})
	require.NoError(t, err)
	_, err = h.settlements.Approve(ctx, created.ID, settlement.AuditRequest{Reason: "通过"})
	require.NoError(t, err)

	// 已通过月份的已完工维修记录删除被锁定端口拦截
	err = h.repairs.Delete(ctx, finished.ID)
	requireConflict(t, err)

	// 第二条: 进行中的维修, 待审核结算单期间不允许在该月完工入账
	deviceB := h.createLamp(t, "LD-T-102")
	faultB, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: deviceB.ID, FaultType: "灯不亮", Description: "结算期间新完工应被拦截",
		ReportedAt: time.Date(2026, 3, 10, 7, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)
	ongoing, err := h.repairs.Create(ctx, repair.CreateRequest{
		FaultID: faultB.ID, Repairman: "维修工乙", RepairTeam: "结算班组",
		StartedAt: time.Date(2026, 3, 10, 8, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)
	_, err = h.repairs.Finish(ctx, ongoing.ID, repair.FinishRequest{
		Result:     repair.ResultFixed,
		FinishedAt: time.Date(2026, 3, 10, 10, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
	})
	requireConflict(t, err)

	// 其它班组同期完工不受影响
	deviceC := h.createLamp(t, "LD-T-103")
	faultC, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: deviceC.ID, FaultType: "灯不亮", Description: "其它班组不受锁定影响",
		ReportedAt: time.Date(2026, 3, 10, 7, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)
	other, err := h.repairs.Create(ctx, repair.CreateRequest{
		FaultID: faultC.ID, Repairman: "维修工丙", RepairTeam: "其它班组",
		StartedAt: time.Date(2026, 3, 10, 8, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)
	_, err = h.repairs.Finish(ctx, other.ID, repair.FinishRequest{
		Result:     repair.ResultFixed,
		FinishedAt: time.Date(2026, 3, 10, 10, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)
}
