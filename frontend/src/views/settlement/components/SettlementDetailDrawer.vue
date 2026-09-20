<template>
  <el-drawer
    :model-value="modelValue"
    title="结算单详情"
    size="780px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="load"
  >
    <div v-loading="loading">
      <template v-if="detail.id">
        <el-descriptions :column="3" border size="small">
          <el-descriptions-item label="结算单号">{{ detail.settle_no }}</el-descriptions-item>
          <el-descriptions-item label="维修班组">{{ detail.repair_team }}</el-descriptions-item>
          <el-descriptions-item label="归集月份">{{ detail.period }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <StatusTag :dict="SETTLEMENT_STATUS" :value="detail.status" />
          </el-descriptions-item>
          <el-descriptions-item label="当前版本">第 {{ detail.version }} 版</el-descriptions-item>
          <el-descriptions-item label="结算金额">
            <span class="amount">{{ formatMoney(detail.total_amount) }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="明细条数">{{ detail.record_count }} 条</el-descriptions-item>
          <el-descriptions-item label="提交时间">{{ formatDateTime(detail.submitted_at) }}</el-descriptions-item>
          <el-descriptions-item label="通过时间">{{ formatDateTime(detail.approved_at) }}</el-descriptions-item>
        </el-descriptions>

        <el-alert
          v-if="detail.stale && detail.status !== 'approved'"
          :closable="false"
          type="warning"
          show-icon
          class="detail-alert"
          :title="`实时归集已变化: 当前为 ${detail.current_count} 条 / ${formatMoney(detail.current_amount)}, 与第 ${detail.version} 版快照不一致。驳回状态下可直接重新提交以生成新版本。`"
        />

        <el-tabs v-model="activeTab" class="detail-tabs">
          <el-tab-pane label="版本明细" name="items">
            <div class="tab-toolbar">
              <span class="text-muted">第 {{ detail.version }} 版快照（提交时固化，不随后续修改变化）</span>
              <span class="amount">合计 {{ formatMoney(detail.total_amount) }}</span>
            </div>
            <el-table :data="detail.items" size="small" border>
              <el-table-column prop="repair_no" label="维修单号" width="150" />
              <el-table-column prop="fault_no" label="故障单号" width="150" />
              <el-table-column prop="lamp_code" label="路灯编号" width="95" />
              <el-table-column prop="repairman" label="维修人员" width="85" />
              <el-table-column label="完工时间" width="135">
                <template #default="{ row }">{{ formatDateTime(row.finished_at) }}</template>
              </el-table-column>
              <el-table-column label="费用" width="100">
                <template #default="{ row }">{{ formatMoney(row.cost) }}</template>
              </el-table-column>
              <el-table-column prop="content" label="维修内容" min-width="130" show-overflow-tooltip />
            </el-table>
          </el-tab-pane>

          <el-tab-pane label="版本对比" name="diff">
            <div class="tab-toolbar">
              <div class="version-pickers">
                <el-select v-model="diffFrom" size="small" style="width: 96px">
                  <el-option v-for="v in versionOptions" :key="v" :label="`第 ${v} 版`" :value="v" />
                </el-select>
                <span class="text-muted">→</span>
                <el-select v-model="diffTo" size="small" style="width: 96px">
                  <el-option v-for="v in versionOptions" :key="v" :label="`第 ${v} 版`" :value="v" />
                </el-select>
                <el-button size="small" type="primary" :loading="diffLoading" @click="loadDiff">对比</el-button>
              </div>
            </div>

            <template v-if="diffResult">
              <el-row :gutter="12" class="diff-summary">
                <el-col :span="8">
                  <el-statistic title="旧版金额" :value="diffResult.from_amount" :precision="2" />
                </el-col>
                <el-col :span="8">
                  <el-statistic title="新版金额" :value="diffResult.to_amount" :precision="2" />
                </el-col>
                <el-col :span="8">
                  <el-statistic
                    title="金额差异 / 条数差异"
                    :value="diffResult.delta_amount"
                    :precision="2"
                    :value-style="{ color: diffResult.delta_amount > 0 ? '#67c23a' : diffResult.delta_amount < 0 ? '#f56c6c' : '#909399' }"
                  >
                    <template #suffix>
                      <span class="text-muted"> / {{ diffResult.delta_count > 0 ? '+' : '' }}{{ diffResult.delta_count }} 条</span>
                    </template>
                  </el-statistic>
                </el-col>
              </el-row>

              <el-table :data="diffResult.items" size="small" border>
                <el-table-column label="类型" width="90">
                  <template #default="{ row }">
                    <el-tag :type="dictType(SETTLEMENT_DIFF_TYPE, row.type)" size="small">
                      {{ dictLabel(SETTLEMENT_DIFF_TYPE, row.type) }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="repair_no" label="维修单号" width="150" />
                <el-table-column prop="lamp_code" label="路灯编号" width="95" />
                <el-table-column prop="repairman" label="维修人员" width="85" />
                <el-table-column label="原费用" width="95">
                  <template #default="{ row }">{{ row.type === 'added' ? '-' : formatMoney(row.from_cost) }}</template>
                </el-table-column>
                <el-table-column label="新费用" width="95">
                  <template #default="{ row }">{{ row.type === 'removed' ? '-' : formatMoney(row.to_cost) }}</template>
                </el-table-column>
                <el-table-column label="差异" width="100">
                  <template #default="{ row }">
                    <span :class="row.delta > 0 ? 'diff-up' : row.delta < 0 ? 'diff-down' : ''">
                      {{ row.delta > 0 ? '+' : '' }}{{ row.delta.toFixed(2) }}
                    </span>
                  </template>
                </el-table-column>
                <el-table-column prop="content" label="维修内容" min-width="120" show-overflow-tooltip />
              </el-table>
              <el-empty v-if="!diffResult.items.length" description="两个版本明细完全一致" :image-size="70" />
            </template>
            <el-empty v-else description="选择两个版本后查看差异" :image-size="80" />
          </el-tab-pane>

          <el-tab-pane label="流转记录" name="flows">
            <el-timeline>
              <el-timeline-item
                v-for="flow in detail.flows"
                :key="flow.id"
                :timestamp="formatDateTime(flow.created_at)"
                :type="dictType(SETTLEMENT_ACTION, flow.action) || 'primary'"
              >
                <div class="flow-title">
                  <el-tag :type="dictType(SETTLEMENT_ACTION, flow.action)" size="small">
                    {{ dictLabel(SETTLEMENT_ACTION, flow.action) }}
                  </el-tag>
                  <span class="flow-version">第 {{ flow.version }} 版</span>
                  <span v-if="flow.operator" class="text-muted">{{ flow.operator }}</span>
                </div>
                <div class="flow-reason">{{ flow.reason || '（无说明）' }}</div>
                <div class="text-muted flow-amount">{{ flow.record_count }} 条 · {{ formatMoney(flow.total_amount) }}</div>
              </el-timeline-item>
            </el-timeline>
          </el-tab-pane>
        </el-tabs>
      </template>
      <el-empty v-else description="暂无结算单数据" />
    </div>
  </el-drawer>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import StatusTag from '@/components/common/StatusTag.vue'
import { settlementApi } from '@/api/settlement'
import {
  SETTLEMENT_ACTION,
  SETTLEMENT_DIFF_TYPE,
  SETTLEMENT_STATUS,
  dictLabel,
  dictType,
} from '@/constants/dict'
import { formatDateTime, formatMoney } from '@/utils/format'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  settlementId: { type: [Number, String], default: null },
})

const loading = ref(false)
const detail = ref({})
const activeTab = ref('items')
const diffFrom = ref(1)
const diffTo = ref(1)
const diffResult = ref(null)
const diffLoading = ref(false)

const versionOptions = computed(() => {
  const list = []
  for (let v = 1; v <= (detail.value.version || 1); v++) list.push(v)
  return list
})

watch(
  () => props.settlementId,
  () => {
    diffResult.value = null
    activeTab.value = 'items'
  },
)

async function load() {
  if (!props.settlementId) return
  loading.value = true
  try {
    detail.value = await settlementApi.detail(props.settlementId)
    diffTo.value = detail.value.version
    diffFrom.value = Math.max(1, detail.value.version - 1)
    if (detail.value.version >= 2) loadDiff()
  } catch (error) {
    detail.value = {}
  } finally {
    loading.value = false
  }
}

async function loadDiff() {
  if (diffFrom.value === diffTo.value) {
    diffResult.value = null
    return
  }
  diffLoading.value = true
  try {
    diffResult.value = await settlementApi.diff(props.settlementId, {
      from_version: diffFrom.value,
      to_version: diffTo.value,
    })
  } finally {
    diffLoading.value = false
  }
}
</script>

<style scoped>
.amount {
  font-weight: 600;
  color: #e6a23c;
}

.detail-alert {
  margin-top: 12px;
}

.detail-tabs {
  margin-top: 4px;
}

.tab-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.version-pickers {
  display: flex;
  align-items: center;
  gap: 8px;
}

.diff-summary {
  margin: 4px 0 14px;
}

.diff-up {
  color: #67c23a;
  font-weight: 600;
}

.diff-down {
  color: #f56c6c;
  font-weight: 600;
}

.flow-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.flow-version {
  font-size: 12px;
  color: #606266;
}

.flow-reason {
  margin-top: 4px;
  font-size: 13px;
}

.flow-amount {
  font-size: 12px;
  margin-top: 2px;
}
</style>
