<template>
  <el-dialog
    :model-value="modelValue"
    title="新建费用结算单"
    width="720px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="handleOpen"
  >
    <el-form :model="form" label-width="92px">
      <el-form-item label="维修班组" required>
        <el-select v-model="form.repair_team" filterable allow-create placeholder="选择或输入班组" style="width: 100%" @change="loadPreview">
          <el-option v-for="item in teams" :key="item" :label="item" :value="item" />
        </el-select>
      </el-form-item>
      <el-form-item label="归集月份" required>
        <el-date-picker
          v-model="form.period"
          type="month"
          value-format="YYYY-MM"
          placeholder="选择完工月份"
          style="width: 100%"
          @change="loadPreview"
        />
      </el-form-item>
    </el-form>

    <div v-loading="previewing" class="preview-block">
      <template v-if="preview">
        <el-alert
          v-if="preview.existing_settle_no"
          :closable="false"
          type="warning"
          show-icon
          class="preview-alert"
          :title="`该班组月份已存在结算单 ${preview.existing_settle_no}（${dictLabel(SETTLEMENT_STATUS, preview.existing_status)}），不能重复建账`"
        />
        <div class="preview-head">
          <span>归集预览（按完工时间所在月，仅已完工维修记录）</span>
          <el-tag type="info">{{ preview.record_count }} 条</el-tag>
          <el-tag type="warning">{{ formatMoney(preview.total_amount) }}</el-tag>
        </div>
        <el-table :data="preview.records" size="small" border max-height="280">
          <el-table-column prop="repair_no" label="维修单号" width="150" />
          <el-table-column prop="fault_no" label="故障单号" width="150" />
          <el-table-column prop="lamp_code" label="路灯编号" width="100" />
          <el-table-column prop="repairman" label="维修人员" width="90" />
          <el-table-column label="完工时间" width="140">
            <template #default="{ row }">{{ formatDateTime(row.finished_at) }}</template>
          </el-table-column>
          <el-table-column label="费用" width="100">
            <template #default="{ row }">{{ formatMoney(row.cost) }}</template>
          </el-table-column>
          <el-table-column prop="content" label="维修内容" min-width="140" show-overflow-tooltip />
        </el-table>
      </template>
      <el-empty v-else-if="loaded" description="选择班组与月份后展示归集明细" :image-size="70" />
    </div>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="handleSubmit">建账（结算金额以系统归集为准）</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { settlementApi } from '@/api/settlement'
import { SETTLEMENT_STATUS, dictLabel } from '@/constants/dict'
import { formatDateTime, formatMoney } from '@/utils/format'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  teams: { type: Array, default: () => [] },
})

const emit = defineEmits(['update:modelValue', 'created'])

const form = reactive({ repair_team: '', period: '' })
const preview = ref(null)
const previewing = ref(false)
const loaded = ref(false)
const submitting = ref(false)
let requestSeq = 0

const canSubmit = computed(() =>
  !!form.repair_team && !!form.period && preview.value && preview.value.record_count > 0 && !preview.value.existing_settle_no,
)

function handleOpen() {
  form.repair_team = ''
  form.period = ''
  preview.value = null
  loaded.value = false
}

async function loadPreview() {
  if (!form.repair_team || !form.period) return
  const seq = ++requestSeq
  previewing.value = true
  try {
    const data = await settlementApi.preview({ repair_team: form.repair_team, period: form.period })
    if (seq === requestSeq) preview.value = data
  } catch (error) {
    if (seq === requestSeq) preview.value = null
  } finally {
    if (seq === requestSeq) {
      previewing.value = false
      loaded.value = true
    }
  }
}

async function handleSubmit() {
  submitting.value = true
  try {
    const data = await settlementApi.create({ repair_team: form.repair_team, period: form.period })
    ElMessage.success(`结算单 ${data.settle_no} 已建账, 归集 ${data.record_count} 条记录, 合计 ${formatMoney(data.total_amount)}`)
    emit('update:modelValue', false)
    emit('created')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.preview-block {
  margin-top: 8px;
}

.preview-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 13px;
  font-weight: 600;
}

.preview-alert {
  margin-bottom: 10px;
}
</style>
