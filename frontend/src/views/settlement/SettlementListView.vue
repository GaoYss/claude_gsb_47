<template>
  <div class="page">
    <PageHeader title="维修费用结算" description="按班组与完工月份归集维修费用, 提交结算单并支持驳回重提, 已结算月份记录自动锁定">
      <el-button :icon="Refresh" @click="reloadAll">刷新</el-button>
      <el-button type="primary" :icon="Plus" @click="openCreate">新建结算单</el-button>
    </PageHeader>

    <el-card shadow="never">
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <!-- ============ 结算单 ============ -->
        <el-tab-pane label="结算单" name="settlements">
          <div class="filter-bar">
            <el-input v-model="settleQuery.keyword" placeholder="结算单号 / 班组" clearable style="width: 220px" @keyup.enter="searchSettlements" />
            <el-select v-model="settleQuery.status" placeholder="状态" clearable style="width: 140px" @change="searchSettlements">
              <el-option v-for="(item, key) in SETTLEMENT_STATUS" :key="key" :label="item.label" :value="key" />
            </el-select>
            <el-select v-model="settleQuery.team" placeholder="班组" clearable filterable style="width: 170px" @change="searchSettlements">
              <el-option v-for="item in meta.teams" :key="item" :label="item" :value="item" />
            </el-select>
            <el-date-picker
              v-model="settleQuery.period"
              type="month"
              value-format="YYYY-MM"
              placeholder="归集月份"
              style="width: 150px"
              @change="searchSettlements"
            />
            <el-button type="primary" :icon="Search" @click="searchSettlements">查询</el-button>
            <el-button :icon="RefreshLeft" @click="resetSettlements">重置</el-button>
          </div>

          <el-table v-loading="settleLoading" :data="settlements" stripe>
            <el-table-column prop="settle_no" label="结算单号" width="150" fixed="left" />
            <el-table-column prop="repair_team" label="维修班组" width="140" />
            <el-table-column prop="period" label="归集月份" width="110" />
            <el-table-column label="状态" width="100">
              <template #default="{ row }"><StatusTag :dict="SETTLEMENT_STATUS" :value="row.status" /></template>
            </el-table-column>
            <el-table-column label="版本" width="80">
              <template #default="{ row }">
                <el-tag v-if="row.version > 1" size="small" type="warning">第 {{ row.version }} 版</el-tag>
                <span v-else class="text-muted">第 1 版</span>
              </template>
            </el-table-column>
            <el-table-column prop="record_count" label="明细条数" width="90" />
            <el-table-column label="结算金额" width="120">
              <template #default="{ row }"><span class="amount">{{ formatMoney(row.total_amount) }}</span></template>
            </el-table-column>
            <el-table-column label="提交时间" width="150">
              <template #default="{ row }">{{ formatDateTime(row.submitted_at) }}</template>
            </el-table-column>
            <el-table-column prop="last_reason" label="最近流转原因" min-width="200" show-overflow-tooltip />
            <el-table-column label="操作" width="240" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openDetail(row)">详情</el-button>
                <el-button v-if="row.status === 'draft'" link type="primary" @click="openFlow(row, 'submit')">提交</el-button>
                <el-button v-if="row.status === 'submitted'" link type="success" @click="openFlow(row, 'approve')">通过</el-button>
                <el-button v-if="row.status === 'submitted'" link type="danger" @click="openFlow(row, 'reject')">驳回</el-button>
                <el-button v-if="row.status === 'rejected'" link type="warning" @click="openFlow(row, 'resubmit')">重新提交</el-button>
                <el-button
                  v-if="row.status === 'draft' || row.status === 'rejected'"
                  link
                  type="danger"
                  @click="handleDelete(row)"
                >删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <DataPagination
            :page="settleQuery.page"
            :page-size="settleQuery.page_size"
            :total="settleTotal"
            @page-change="changeSettlePage"
            @size-change="changeSettleSize"
          />
        </el-tab-pane>

        <!-- ============ 费用台账 ============ -->
        <el-tab-pane label="费用台账" name="ledger">
          <div class="ledger-stats">
            <StatCard label="台账总金额" :value="formatMoney(summary.total_amount)" :hint="`${summary.total_count} 条已完工维修记录`" color="#303133" />
            <StatCard label="已结算金额" :value="formatMoney(summary.settled_amount)" :hint="`${summary.settled_count} 条 · 月份已锁定`" color="#67c23a" />
            <StatCard label="待结算金额" :value="formatMoney(summary.pending_amount)" :hint="`${summary.pending_count} 条 · 可建账`" color="#e6a23c" />
          </div>

          <div class="filter-bar">
            <el-input v-model="ledgerQuery.keyword" placeholder="维修单号 / 故障单号 / 路灯编号 / 维修人员" clearable style="width: 260px" @keyup.enter="searchLedger" />
            <el-select v-model="ledgerQuery.repair_team" placeholder="班组" clearable filterable style="width: 160px" @change="searchLedger">
              <el-option v-for="item in meta.teams" :key="item" :label="item" :value="item" />
            </el-select>
            <el-date-picker
              v-model="ledgerQuery.period"
              type="month"
              value-format="YYYY-MM"
              placeholder="完工月份"
              style="width: 150px"
              @change="searchLedger"
            />
            <el-radio-group v-model="ledgerQuery.settled" @change="searchLedger">
              <el-radio-button value="">全部</el-radio-button>
              <el-radio-button value="yes">已结算月份</el-radio-button>
              <el-radio-button value="no">待结算月份</el-radio-button>
            </el-radio-group>
            <el-button type="primary" :icon="Search" @click="searchLedger">查询</el-button>
            <el-button :icon="RefreshLeft" @click="resetLedger">重置</el-button>
          </div>

          <el-table v-loading="ledgerLoading" :data="ledgerRows" stripe>
            <el-table-column prop="repair_no" label="维修单号" width="150" fixed="left" />
            <el-table-column prop="fault_no" label="故障单号" width="150" />
            <el-table-column prop="lamp_code" label="路灯编号" width="100" />
            <el-table-column prop="repairman" label="维修人员" width="90" />
            <el-table-column prop="repair_team" label="班组" width="130" />
            <el-table-column prop="period" label="完工月份" width="100" />
            <el-table-column label="完工时间" width="150">
              <template #default="{ row }">{{ formatDateTime(row.finished_at) }}</template>
            </el-table-column>
            <el-table-column label="费用" width="110">
              <template #default="{ row }"><span class="amount">{{ formatMoney(row.cost) }}</span></template>
            </el-table-column>
            <el-table-column label="结算状态" width="110">
              <template #default="{ row }">
                <el-tooltip v-if="row.settle_no" :content="`${row.settle_no}`" placement="top">
                  <StatusTag :dict="SETTLEMENT_STATUS" :value="row.settlement_status" />
                </el-tooltip>
                <el-tag v-else size="small" type="info" effect="plain">未建账</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="锁定" width="80">
              <template #default="{ row }">
                <el-tag v-if="row.locked" size="small" type="danger">已锁定</el-tag>
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="content" label="维修内容" min-width="160" show-overflow-tooltip />
            <el-table-column label="操作" width="100" fixed="right">
              <template #default="{ row }">
                <el-button v-if="row.settlement_id" link type="primary" @click="openSettlementById(row.settlement_id)">结算单</el-button>
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
          </el-table>
          <DataPagination
            :page="ledgerQuery.page"
            :page-size="ledgerQuery.page_size"
            :total="ledgerTotal"
            @page-change="changeLedgerPage"
            @size-change="changeLedgerSize"
          />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <CreateSettlementDialog v-model="createVisible" :teams="meta.teams" @created="handleCreated" />
    <SettlementFlowDialog v-model="flowVisible" :model="flowTarget" :action="flowAction" @saved="handleFlowSaved" />
    <SettlementDetailDrawer v-model="detailVisible" :settlement-id="detailId" />
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, RefreshLeft, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/common/PageHeader.vue'
import StatusTag from '@/components/common/StatusTag.vue'
import StatCard from '@/components/common/StatCard.vue'
import DataPagination from '@/components/common/DataPagination.vue'
import CreateSettlementDialog from './components/CreateSettlementDialog.vue'
import SettlementFlowDialog from './components/SettlementFlowDialog.vue'
import SettlementDetailDrawer from './components/SettlementDetailDrawer.vue'
import { settlementApi } from '@/api/settlement'
import { SETTLEMENT_STATUS } from '@/constants/dict'
import { formatDateTime, formatMoney } from '@/utils/format'

const activeTab = ref('settlements')

const meta = ref({ teams: [], periods: [], statuses: [] })

// ---------- 结算单列表 ----------
const settleLoading = ref(false)
const settlements = ref([])
const settleTotal = ref(0)
const settleQuery = reactive({ page: 1, page_size: 10, keyword: '', status: '', team: '', period: '' })

async function loadSettlements() {
  settleLoading.value = true
  try {
    const data = await settlementApi.list({ ...settleQuery })
    settlements.value = data.items ?? []
    settleTotal.value = data.total ?? 0
  } finally {
    settleLoading.value = false
  }
}
function searchSettlements() { settleQuery.page = 1; loadSettlements() }
function resetSettlements() {
  Object.assign(settleQuery, { page: 1, page_size: 10, keyword: '', status: '', team: '', period: '' })
  loadSettlements()
}
function changeSettlePage(page) { settleQuery.page = page; loadSettlements() }
function changeSettleSize(size) { settleQuery.page = 1; settleQuery.page_size = size; loadSettlements() }

// ---------- 费用台账 ----------
const ledgerLoading = ref(false)
const ledgerRows = ref([])
const ledgerTotal = ref(0)
const summary = ref({ total_count: 0, total_amount: 0, settled_count: 0, settled_amount: 0, pending_count: 0, pending_amount: 0 })
const ledgerQuery = reactive({ page: 1, page_size: 10, keyword: '', repair_team: '', period: '', settled: '' })

async function loadLedger() {
  ledgerLoading.value = true
  try {
    const data = await settlementApi.ledger({ ...ledgerQuery })
    ledgerRows.value = data.items ?? []
    ledgerTotal.value = data.total ?? 0
    summary.value = data.summary ?? summary.value
  } finally {
    ledgerLoading.value = false
  }
}
function searchLedger() { ledgerQuery.page = 1; loadLedger() }
function resetLedger() {
  Object.assign(ledgerQuery, { page: 1, page_size: 10, keyword: '', repair_team: '', period: '', settled: '' })
  loadLedger()
}
function changeLedgerPage(page) { ledgerQuery.page = page; loadLedger() }
function changeLedgerSize(size) { ledgerQuery.page = 1; ledgerQuery.page_size = size; loadLedger() }

// ---------- 弹窗与操作 ----------
const createVisible = ref(false)
const flowVisible = ref(false)
const flowTarget = ref(null)
const flowAction = ref('submit')
const detailVisible = ref(false)
const detailId = ref(null)

function openCreate() { createVisible.value = true }
function openFlow(row, action) { flowTarget.value = { ...row }; flowAction.value = action; flowVisible.value = true }
function openDetail(row) { detailId.value = row.id; detailVisible.value = true }
function openSettlementById(id) { detailId.value = id; detailVisible.value = true }

function handleCreated() {
  loadSettlements()
  loadMeta()
}

async function handleFlowSaved() {
  await Promise.all([loadSettlements(), loadMeta()])
  if (activeTab.value === 'ledger') loadLedger()
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确认删除结算单 ${row.settle_no} ? 其明细快照与流转记录将一并删除。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '确认删除',
      cancelButtonText: '取消',
    })
  } catch (error) {
    return
  }
  await settlementApi.remove(row.id)
  ElMessage.success('结算单已删除')
  loadSettlements()
}

function handleTabChange(name) {
  if (name === 'ledger') loadLedger()
}

function reloadAll() {
  loadMeta()
  loadSettlements()
  if (activeTab.value === 'ledger') loadLedger()
}

async function loadMeta() {
  try {
    meta.value = await settlementApi.meta()
  } catch (error) {
    // 提示由拦截器处理
  }
}

onMounted(() => {
  loadMeta()
  loadSettlements()
})
</script>

<style scoped>
.amount {
  font-weight: 600;
  color: #e6a23c;
}

.ledger-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-bottom: 14px;
}
</style>
