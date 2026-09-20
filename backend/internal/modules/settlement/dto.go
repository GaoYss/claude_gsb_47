package settlement

import (
	"streetlight/internal/modules/repair"
	"streetlight/pkg/pagination"
)

// CreateRequest 建账(创建结算单)请求, 按班组与完工月份自动归集。
type CreateRequest struct {
	RepairTeam string `json:"repair_team" binding:"required,max=64"`
	Period     string `json:"period" binding:"required,len=7"` // YYYY-MM
}

// SubmitRequest 提交 / 重新提交结算单。
type SubmitRequest struct {
	Reason   string `json:"reason" binding:"omitempty,max=255"`
	Operator string `json:"operator" binding:"omitempty,max=64"`
}

// AuditRequest 审核(驳回 / 通过)请求。
type AuditRequest struct {
	Reason   string `json:"reason" binding:"omitempty,max=255"`
	Operator string `json:"operator" binding:"omitempty,max=64"`
}

// ListQuery 结算单分页查询条件。
type ListQuery struct {
	pagination.Params
	Keyword string `form:"keyword"` // 结算单号 / 班组
	Status  string `form:"status"`
	Team    string `form:"team"`
	Period  string `form:"period"` // YYYY-MM
}

// RepairRecordQuery 费用台账(维修记录)查询条件。
type RepairRecordQuery struct {
	pagination.Params
	Keyword    string `form:"keyword"` // 维修单号 / 故障单号 / 路灯编号 / 维修人员
	RepairTeam string `form:"repair_team"`
	Period     string `form:"period"` // 完工月份 YYYY-MM
	StartDate  string `form:"start_date"`
	EndDate    string `form:"end_date"`
	Settled    string `form:"settled"` // "" 不过滤 / yes 已结算月份 / no 未结算月份
}

// Meta 结算模块字典与可建账班组。
type Meta struct {
	Statuses []string `json:"statuses"`
	Actions  []string `json:"actions"`
	Teams    []string `json:"teams"`
	Periods  []string `json:"periods"` // 存在已完工维修记录的月份, 倒序
}

// LedgerSummary 费用台账汇总。
type LedgerSummary struct {
	TotalCount    int64   `json:"total_count"`
	TotalAmount   float64 `json:"total_amount"`
	SettledCount  int64   `json:"settled_count"`
	SettledAmount float64 `json:"settled_amount"`
	PendingCount  int64   `json:"pending_count"`
	PendingAmount float64 `json:"pending_amount"`
}

// RepairRecord 台账中的一条维修费用记录, 附带其完工月份的结算状态。
type RepairRecord struct {
	ID               uint    `json:"id"`
	RepairNo         string  `json:"repair_no"`
	FaultNo          string  `json:"fault_no"`
	LampCode         string  `json:"lamp_code"`
	Repairman        string  `json:"repairman"`
	RepairTeam       string  `json:"repair_team"`
	FinishedAt       string  `json:"finished_at"`
	Period           string  `json:"period"`
	Cost             float64 `json:"cost"`
	Content          string  `json:"content"`
	Materials        string  `json:"materials"`
	SettlementID     *uint   `json:"settlement_id"`
	SettleNo         string  `json:"settle_no"`
	SettlementStatus string  `json:"settlement_status"` // approved / submitted / ... / "" 未建账
	Locked           bool    `json:"locked"`            // 所在班组月份已审核通过, 记录不允许改动
}

// Detail 结算单详情: 主信息 + 当前版本明细 + 历史流水。
type Detail struct {
	*Settlement
	Items []SettlementItem `json:"items"`
	Flows []SettlementFlow `json:"flows"`
	// CurrentAmount 为当前实时归集金额(可能与已提交版本不一致, 提示重新归集)。
	CurrentAmount float64 `json:"current_amount"`
	CurrentCount  int     `json:"current_count"`
	Stale         bool    `json:"stale"` // 实时归集与当前版本快照不一致
}

// ItemDiff 单个明细在两个版本之间的差异。
type ItemDiff struct {
	Type      string  `json:"type"` // added / removed / changed
	RepairID  uint    `json:"repair_id"`
	RepairNo  string  `json:"repair_no"`
	FaultNo   string  `json:"fault_no"`
	LampCode  string  `json:"lamp_code"`
	Repairman string  `json:"repairman"`
	FromCost  float64 `json:"from_cost"`
	ToCost    float64 `json:"to_cost"`
	Delta     float64 `json:"delta"`
	Content   string  `json:"content"`
}

// VersionDiff 两个版本之间的差异对比。
type VersionDiff struct {
	FromVersion int        `json:"from_version"`
	ToVersion   int        `json:"to_version"`
	FromAmount  float64    `json:"from_amount"`
	ToAmount    float64    `json:"to_amount"`
	DeltaAmount float64    `json:"delta_amount"`
	FromCount   int        `json:"from_count"`
	ToCount     int        `json:"to_count"`
	DeltaCount  int        `json:"delta_count"`
	Items       []ItemDiff `json:"items"`
}

// PreviewResult 建账前的归集预览结果。
type PreviewResult struct {
	RepairTeam       string          `json:"repair_team"`
	Period           string          `json:"period"`
	RecordCount      int             `json:"record_count"`
	TotalAmount      float64         `json:"total_amount"`
	Records          []repair.Repair `json:"records"`
	ExistingSettleNo string          `json:"existing_settle_no,omitempty"`
	ExistingStatus   string          `json:"existing_status,omitempty"`
}
