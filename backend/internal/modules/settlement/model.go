package settlement

import "time"

// 结算单状态。
const (
	StatusDraft     = "draft"     // 草稿(可继续归集)
	StatusSubmitted = "submitted" // 待审核
	StatusApproved  = "approved"  // 已通过(对应月份的维修记录被锁定)
	StatusRejected  = "rejected"  // 已驳回(可修改后重新提交)
)

// 流转动作。
const (
	ActionCreate   = "create"   // 建账
	ActionSubmit   = "submit"   // 提交
	ActionReject   = "reject"   // 驳回
	ActionResubmit = "resubmit" // 重新提交
	ActionApprove  = "approve"  // 审核通过
	ActionRebuild  = "rebuild"  // 重新归集
)

// Statuses 返回全部结算单状态。
func Statuses() []string {
	return []string{StatusDraft, StatusSubmitted, StatusApproved, StatusRejected}
}

// Actions 返回全部流转动作。
func Actions() []string {
	return []string{ActionCreate, ActionSubmit, ActionReject, ActionResubmit, ActionApprove, ActionRebuild}
}

// IsValidStatus 校验结算单状态取值。
func IsValidStatus(status string) bool {
	for _, item := range Statuses() {
		if item == status {
			return true
		}
	}
	return false
}

// Settlement 维修费用结算单, 一张结算单对应「一个维修班组 + 一个完工月份」。
type Settlement struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	SettleNo   string `gorm:"size:64;uniqueIndex;not null" json:"settle_no"`
	RepairTeam string `gorm:"size:64;uniqueIndex:idx_team_period;index;not null" json:"repair_team"`
	// Period 为归集月份, 格式 YYYY-MM, 以维修记录的完工时间所在月归集。
	Period      string     `gorm:"size:7;uniqueIndex:idx_team_period;index;not null" json:"period"`
	Status      string     `gorm:"size:32;index;not null;default:draft" json:"status"`
	Version     int        `gorm:"not null;default:1" json:"version"`
	RecordCount int        `gorm:"not null;default:0" json:"record_count"`
	TotalAmount float64    `gorm:"not null;default:0" json:"total_amount"`
	LastReason  string     `gorm:"size:255" json:"last_reason"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TableName 指定表名。
func (Settlement) TableName() string { return "settlement" }

// SettlementItem 结算单明细, 每次提交生成一个版本的快照, 驳回重提后保留历史版本用于差异对比。
type SettlementItem struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	SettlementID uint      `gorm:"index:idx_settlement_version;not null" json:"settlement_id"`
	Version      int       `gorm:"index:idx_settlement_version;not null" json:"version"`
	RepairID     uint      `gorm:"index;not null" json:"repair_id"`
	RepairNo     string    `gorm:"size:64;not null" json:"repair_no"`
	FaultNo      string    `gorm:"size:64" json:"fault_no"`
	LampCode     string    `gorm:"size:64" json:"lamp_code"`
	Repairman    string    `gorm:"size:64" json:"repairman"`
	FinishedAt   time.Time `gorm:"not null" json:"finished_at"`
	Cost         float64   `gorm:"not null;default:0" json:"cost"`
	Content      string    `gorm:"size:512" json:"content"`
	Materials    string    `gorm:"size:255" json:"materials"`
	CreatedAt    time.Time `json:"created_at"`
}

// TableName 指定表名。
func (SettlementItem) TableName() string { return "settlement_item" }

// SettlementFlow 结算单流转流水, 记录每次提交/驳回/重提/通过的动作、原因与当时金额。
type SettlementFlow struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	SettlementID uint      `gorm:"index;not null" json:"settlement_id"`
	Version      int       `gorm:"not null" json:"version"`
	Action       string    `gorm:"size:32;index;not null" json:"action"`
	Reason       string    `gorm:"size:255" json:"reason"`
	Operator     string    `gorm:"size:64" json:"operator"`
	RecordCount  int       `gorm:"not null;default:0" json:"record_count"`
	TotalAmount  float64   `gorm:"not null;default:0" json:"total_amount"`
	CreatedAt    time.Time `json:"created_at"`
}

// TableName 指定表名。
func (SettlementFlow) TableName() string { return "settlement_flow" }
