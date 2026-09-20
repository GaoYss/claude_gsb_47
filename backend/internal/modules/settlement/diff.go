package settlement

import "sort"

// buildVersionDiff 对比两个版本的明细快照:
// 以维修记录 ID 为关联键, 识别新增(added)/移除(removed)/费用变更(changed)。
func buildVersionDiff(fromVersion, toVersion int, fromItems, toItems []SettlementItem) *VersionDiff {
	fromMap := make(map[uint]SettlementItem, len(fromItems))
	toMap := make(map[uint]SettlementItem, len(toItems))
	for _, item := range fromItems {
		fromMap[item.RepairID] = item
	}
	for _, item := range toItems {
		toMap[item.RepairID] = item
	}

	diffs := make([]ItemDiff, 0)

	for _, item := range toItems {
		previous, exists := fromMap[item.RepairID]
		if !exists {
			diffs = append(diffs, ItemDiff{
				Type:      "added",
				RepairID:  item.RepairID,
				RepairNo:  item.RepairNo,
				FaultNo:   item.FaultNo,
				LampCode:  item.LampCode,
				Repairman: item.Repairman,
				ToCost:    item.Cost,
				Delta:     round2(item.Cost),
				Content:   item.Content,
			})
			continue
		}
		if item.Cost != previous.Cost {
			diffs = append(diffs, ItemDiff{
				Type:      "changed",
				RepairID:  item.RepairID,
				RepairNo:  item.RepairNo,
				FaultNo:   item.FaultNo,
				LampCode:  item.LampCode,
				Repairman: item.Repairman,
				FromCost:  previous.Cost,
				ToCost:    item.Cost,
				Delta:     round2(item.Cost - previous.Cost),
				Content:   item.Content,
			})
		}
	}

	for _, item := range fromItems {
		if _, exists := toMap[item.RepairID]; exists {
			continue
		}
		diffs = append(diffs, ItemDiff{
			Type:      "removed",
			RepairID:  item.RepairID,
			RepairNo:  item.RepairNo,
			FaultNo:   item.FaultNo,
			LampCode:  item.LampCode,
			Repairman: item.Repairman,
			FromCost:  item.Cost,
			Delta:     round2(-item.Cost),
			Content:   item.Content,
		})
	}

	// 稳定排序: 先按变更类型(新增/变更/移除), 再按维修单号。
	order := map[string]int{"added": 0, "changed": 1, "removed": 2}
	sort.SliceStable(diffs, func(i, j int) bool {
		if diffs[i].Type != diffs[j].Type {
			return order[diffs[i].Type] < order[diffs[j].Type]
		}
		return diffs[i].RepairNo < diffs[j].RepairNo
	})

	var fromAmount, toAmount float64
	for _, item := range fromItems {
		fromAmount += item.Cost
	}
	for _, item := range toItems {
		toAmount += item.Cost
	}

	return &VersionDiff{
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		FromAmount:  round2(fromAmount),
		ToAmount:    round2(toAmount),
		DeltaAmount: round2(toAmount - fromAmount),
		FromCount:   len(fromItems),
		ToCount:     len(toItems),
		DeltaCount:  len(toItems) - len(fromItems),
		Items:       diffs,
	}
}
