<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    width="560px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-descriptions v-if="model" :column="2" border size="small" class="flow-summary">
      <el-descriptions-item label="结算单号">{{ model.settle_no }}</el-descriptions-item>
      <el-descriptions-item label="班组 / 月份">{{ model.repair_team }} · {{ model.period }}</el-descriptions-item>
      <el-descriptions-item label="当前版本">第 {{ model.version }} 版</el-descriptions-item>
      <el-descriptions-item label="当前状态">
        <StatusTag :dict="SETTLEMENT_STATUS" :value="model.status" />
      </el-descriptions-item>
    </el-descriptions>

    <el-alert
      v-if="action === 'resubmit'"
      :closable="false"
      type="info"
      show-icon
      class="flow-tip"
      title="重新提交将按当前维修记录重新归集, 自动生成新版本, 上一版明细与差异会完整保留。"
    />

    <el-form ref="formRef" :model="form" :rules="rules" label-width="84px">
      <el-form-item label="操作人" prop="operator">
        <el-input v-model="form.operator" maxlength="64" placeholder="选填, 如 财务-周敏" />
      </el-form-item>
      <el-form-item :label="reasonLabel" prop="reason">
        <el-input
          v-model="form.reason"
          type="textarea"
          :rows="3"
          maxlength="255"
          show-word-limit
          :placeholder="reasonPlaceholder"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button :type="confirmType" :loading="submitting" @click="handleSubmit">{{ title }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import StatusTag from '@/components/common/StatusTag.vue'
import { settlementApi } from '@/api/settlement'
import { SETTLEMENT_STATUS } from '@/constants/dict'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  model: { type: Object, default: null },
  // submit / reject / resubmit / approve
  action: { type: String, default: 'submit' },
})

const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref(null)
const submitting = ref(false)
const form = reactive({ operator: '', reason: '' })

const titleMap = {
  submit: '提交结算单',
  reject: '驳回结算单',
  resubmit: '驳回后重新提交',
  approve: '审核通过',
}
const title = computed(() => titleMap[props.action] ?? '结算操作')
const confirmType = computed(() => (props.action === 'reject' ? 'danger' : 'primary'))
const reasonRequired = computed(() => props.action === 'reject')
const reasonLabel = computed(() => (reasonRequired.value ? '驳回原因' : '说明'))
const reasonPlaceholder = computed(() =>
  reasonRequired.value ? '必填: 说明驳回理由, 该原因会保留在流转记录中' : '选填: 补充说明, 将保留在流转记录中',
)

const rules = computed(() =>
  reasonRequired.value ? { reason: [{ required: true, message: '驳回时必须填写驳回原因', trigger: 'blur' }] } : {},
)

function syncForm() {
  form.operator = ''
  form.reason = ''
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  const payload = { operator: form.operator, reason: form.reason }
  const apiMap = {
    submit: settlementApi.submit,
    reject: settlementApi.reject,
    resubmit: settlementApi.resubmit,
    approve: settlementApi.approve,
  }
  submitting.value = true
  try {
    const data = await apiMap[props.action](props.model.id, payload)
    ElMessage.success('操作成功')
    emit('update:modelValue', false)
    emit('saved', data)
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.flow-summary {
  margin-bottom: 14px;
}

.flow-tip {
  margin-bottom: 12px;
}
</style>
