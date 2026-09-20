package settlement

var statusLabels = map[string]string{
	StatusDraft:     "草稿",
	StatusSubmitted: "待审核",
	StatusApproved:  "已通过",
	StatusRejected:  "已驳回",
}

var actionLabels = map[string]string{
	ActionCreate:   "建账",
	ActionSubmit:   "提交",
	ActionReject:   "驳回",
	ActionResubmit: "重新提交",
	ActionApprove:  "审核通过",
	ActionRebuild:  "重新归集",
}

// StatusLabel 返回结算单状态的中文名称。
func StatusLabel(status string) string {
	if label, ok := statusLabels[status]; ok {
		return label
	}
	return status
}

// ActionLabel 返回流转动作的中文名称。
func ActionLabel(action string) string {
	if label, ok := actionLabels[action]; ok {
		return label
	}
	return action
}
