# 路灯故障登记系统

基于 **Gin + Vue 3** 的前后端分离路灯运维管理系统, 覆盖市政路灯的 **台账管理 → 故障上报 → 维修录入 → 状态查询** 全流程。

| 层次 | 技术选型 |
| --- | --- |
| 后端 | Go 1.27 / Gin / GORM / sqlite(纯 Go 驱动) 或 PostgreSQL |
| 前端 | Vue 3 / Vite / Element Plus / Pinia / Vue Router / Axios |
| 部署 | Docker Compose, Nginx 托管静态资源并反向代理 `/api` |

---

## 一、功能模块

| 模块 | 说明 | 关键能力 |
| --- | --- | --- |
| 路灯台账 | 路灯基础档案维护 | 编号/道路/运行状态/灯具类型筛选, 按道路与状态统计, 删除前校验未闭环故障 |
| 故障登记 | 故障工单登记与流转 | 自动生成故障单号, 紧急程度分级, 状态机流转, 闭环关闭 |
| 维修记录录入 | 针对故障单登记维修过程 | 自动生成维修单号, 开工/完工时间, 耗材与费用, 四种维修结果 |
| 维修费用结算 | 维修费用台账与按月结算 | 按班组+完工月份归集费用, 提交/驳回/重提留痕, 版本快照与差异对比, 已结算月份锁定 |
| 维修状态查询 | 只读的综合看板与追踪视图 | 全局概览, 路灯级维修状态列表, 按故障单号/路灯编号的全链路时间线 |

五个模块在后端是五个平级的独立包 (`internal/modules/{lamp,fault,repair,settlement,status}`), 在前端是五组独立的视图与子组件, 互不耦合; 新增一个业务模块只需要实现 `module.Module` 接口并注册。

---

## 二、目录结构

```text
.
├── backend/                              # Go + Gin 后端
│   ├── cmd/server/main.go                # 进程入口: 配置 -> 应用 -> HTTP -> 优雅退出
│   ├── internal/
│   │   ├── apperr/                       # 业务错误(apperr), 统一映射到 HTTP 状态码
│   │   ├── bootstrap/                    # 装配层: 路由/中间件/模块注册/演示数据
│   │   │   ├── bootstrap.go              # 引擎、探针、/api/v1 模块挂载
│   │   │   ├── modules.go               # 模块构造与依赖注入(避免构造循环)
│   │   │   └── seed.go                   # 演示数据(道路/路灯/故障/维修)
│   │   ├── config/                       # 环境变量配置(可选 .env 兜底)
│   │   ├── database/                     # GORM 连接与自动迁移
│   │   ├── httpx/                        # JSON/Query/路径参数绑定(中文校验提示)
│   │   ├── logging/                      # slog 初始化
│   │   ├── middleware/                   # requestid / logger / recovery / cors
│   │   ├── module/module.go              # Module 接口: Name / Models / RegisterRoutes
│   │   ├── modules/                      # ★ 业务模块, 每个模块一套分层
│   │   │   ├── lamp/                     #   路灯台账
│   │   │   ├── fault/                    #   故障登记
│   │   │   ├── repair/                   #   维修记录录入
│   │   │   ├── settlement/               #   维修费用结算(台账/结算单/版本快照/流转)
│   │   │   └── status/                   #   维修状态查询(只读, 不建表)
│   │   ├── response/                     # 统一响应信封 Envelope / PageData / OK / Fail
│   ├── pkg/pagination/                   # 分页与排序参数(排序字段白名单)
│   ├── data/                             # sqlite 数据文件(运行期生成, 已 gitignore)
│   ├── Dockerfile
│   └── go.mod
│
├── frontend/                             # Vue 3 前端
│   ├── src/
│   │   ├── api/                          # 按模块拆分的接口封装 + axios 实例
│   │   ├── components/
│   │   │   ├── common/                   # PageHeader / StatusTag / DataPagination / StatCard / BarList
│   │   │   └── layout/AppLayout.vue      # 侧边导航 + 内容区骨架
│   │   ├── composables/useListPage.js    # 列表页公共逻辑(查询/分页/加载)
│   │   ├── constants/dict.js             # 与后端一致的枚举字典与颜色
│   │   ├── router/index.js               # 路由表, 每个功能一个路由
│   │   ├── stores/dict.js                # Pinia 字典缓存
│   │   ├── utils/format.js               # 时间/耗时/金额格式化
│   │   └── views/                        # ★ 按功能拆分的页面
│   │       ├── dashboard/DashboardView.vue
│   │       ├── lamp/LampListView.vue + components/LampFormDialog.vue
│   │       ├── fault/FaultListView.vue + components/{FaultFormDialog,FaultDetailDrawer}.vue
│   │       ├── repair/RepairListView.vue + components/{RepairFormDialog,FinishRepairDialog}.vue
│   │       ├── settlement/SettlementListView.vue + components/{CreateSettlementDialog,SettlementFlowDialog,SettlementDetailDrawer}.vue
│   │       └── status/{StatusLampView,StatusTrackView}.vue
│   ├── nginx.conf                        # 生产: SPA 回退 + /api 反向代理
│   ├── Dockerfile
│   └── vite.config.js
│
├── docker-compose.yml                    # 一键启动(含可选 postgres profile)
└── README.MD
```

---

## 三、后端设计

### 3.1 模块内分层

每个业务模块内部都是同一套五层结构, 职责单一、便于替换:

```text
modules/<name>/
├── model.go        # GORM 实体 + 枚举常量 + 状态机校验
├── dto.go          # 创建/更新/查询请求体与列表响应体(带校验标签)
├── repository.go   # 数据访问: 只拼 SQL/查询, 不含业务判断
├── service.go      # 业务规则: 单号生成、状态机、跨模块联动
├── handler.go      # HTTP 层: 绑定参数 -> 调 service -> 统一响应
└── module.go       # 装配: 组装 repo/service/handler 并注册路由
```

`status` 模块是纯只读视图, 复用其它模块的 Repository, 因此没有 `model.go`/`repository.go`, `Models()` 返回 `nil`(不参与自动迁移)。

### 3.2 模块装配与依赖方向

`bootstrap/modules.go` 负责按依赖顺序构造模块, 跨模块只允许依赖「下游端口」, 不反向依赖:

```text
lamp.New(db)                                   -> 路灯台账(无依赖)
fault.New(db, lampService)                     -> 故障登记(读取路灯校验)
repair.New(db, faultService)                   -> 维修记录(驱动故障状态流转)
settlement.New(db)                             -> 费用结算(归集维修费用)
repair.Service().SetSettlementLock(settlement) -> 已结算月份反向锁定维修记录
status.New(db, lampRepo, faultRepo, repairRepo)-> 维修状态查询(只读聚合)
```

路灯运行状态需要根据故障反向回填, 但 `lamp` 不能依赖 `fault`, 因此用 `lampService.SetOpenFaultCounter(faultModule.Repository())` 注入一个统计端口; 同理, 已结算月份不允许改动维修记录, 用 `repairService.SetSettlementLock(settlementModule.Service())` 注入锁定端口, 均避免构造函数循环依赖。

### 3.3 统一响应与错误

所有接口返回统一信封, 前端只需判断 HTTP 状态码 + `code`:

```json
{ "code": "OK", "message": "success", "data": { } }
```

| code | HTTP | 含义 |
| --- | --- | --- |
| `OK` | 200 / 201 | 成功 |
| `INVALID_ARGUMENT` | 400 | 参数或业务校验失败 |
| `NOT_FOUND` | 404 | 资源不存在 |
| `CONFLICT` | 409 | 状态冲突(如重复开工、已关闭不可再操作) |
| `INTERNAL_ERROR` | 500 | 服务端内部错误 |

列表接口统一分页结构: `{ items, total, page, page_size, total_pages }`, 查询参数 `page` / `page_size` / `sort_by` / `order`; 排序字段走白名单, 避免 SQL 注入。删除成功返回 `204 No Content`。

---

## 四、前端设计

- 一个功能一个路由 + 一个视图, 弹窗/抽屉拆成独立子组件 (`views/<feature>/components/`), 列表页公共逻辑收敛到 `composables/useListPage.js`。
- `api/` 每个模块一个文件, `api/request.js` 统一处理 baseURL、超时、响应解包与错误提示。
- 枚举字典集中在 `constants/dict.js`, 与后端常量一一对应, 通过 `stores/dict.js` 缓存字典接口返回值。
- 开发环境通过 Vite 代理访问后端, 生产环境通过 Nginx 同源反代, 前端代码无需区分环境。

---

## 五、快速开始

### 方式一: Docker Compose(推荐)

```bash
docker compose up -d --build
```

启动后:

| 服务 | 地址 | 说明 |
| --- | --- | --- |
| Web 前端 | http://localhost:8081 | Nginx 托管的 Vue 3 单页应用 |
| 后端 API | http://localhost:8080/api/v1 | 也可以只访问前端, 由 Nginx 反代 |
| 健康检查 | http://localhost:8080/healthz / /readyz | 存活 / 就绪探针 |

宿主机端口默认是 API `8080` / Web `8081`。如果本机这两个端口已被其它服务占用, 用 `BACKEND_PORT` / `FRONTEND_PORT` 覆盖即可:

```bash
# Linux / macOS
BACKEND_PORT=18088 FRONTEND_PORT=18082 docker compose up -d --build

# Windows PowerShell
$env:BACKEND_PORT="18088"; $env:FRONTEND_PORT="18082"; docker compose up -d --build
```

```bash
docker compose ps          # 查看状态(backend 应显示 healthy)
docker compose logs -f     # 查看日志
docker compose down        # 停止(保留数据卷)
docker compose down -v     # 停止并删除数据
```

默认使用 **sqlite**, 数据文件位于命名卷 `streetlight-data`。若要用 PostgreSQL:

```bash
DB_DRIVER=postgres \
DB_DSN="host=postgres user=streetlight password=streetlight dbname=streetlight port=5432 sslmode=disable TimeZone=Asia/Shanghai" \
docker compose --profile postgres up -d --build
```

> PowerShell 下把 `\` 换成反引号续行, 或先 `$env:DB_DRIVER="postgres"` 再执行 `docker compose`。

### 方式二: 本地开发

后端(默认监听 8080, 自动创建 `backend/data/streetlight.db`):

```bash
cd backend
go mod download
go run ./cmd/server
```

前端(默认监听 5173, 已配置 `/api` 代理到 `http://127.0.0.1:8080`):

```bash
cd frontend
npm install
npm run dev        # 开发服务器
npm run build      # 产物输出到 dist/
npm run preview    # 预览构建产物
```

本地跑测试与静态检查:

```bash
cd backend
go vet ./...
go test ./...
```

---

## 六、环境变量(后端)

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `APP_NAME` | `streetlight-fault-system` | 应用名, 出现在健康检查响应中 |
| `APP_ENV` | `development` | 运行环境, `production` 时降低日志详细度 |
| `APP_SEED` | `true` | 是否写入演示数据(库中已有数据时自动跳过) |
| `SERVER_HOST` | `0.0.0.0` | 监听地址 |
| `SERVER_PORT` | `8080` | 监听端口 |
| `SERVER_READ_TIMEOUT` | `15s` | 读超时 |
| `SERVER_WRITE_TIMEOUT` | `30s` | 写超时 |
| `CORS_ALLOW_ORIGINS` | `*` | 允许的跨域来源, 逗号分隔 |
| `DB_DRIVER` | `sqlite` | `sqlite` 或 `postgres` |
| `DB_DSN` | `data/streetlight.db` | sqlite 为文件路径, postgres 为连接串 |
| `DB_LOG_LEVEL` | `warn` | GORM 日志级别 |

前端变量放在 `frontend/.env.development` / `.env.production`, 只有 `VITE_API_BASE_URL` 与 `VITE_DEV_PROXY_TARGET`。

> Docker Compose 还有 `BACKEND_PORT`(默认 8080)、`FRONTEND_PORT`(默认 8081)、`DB_DRIVER`、`DB_DSN` 四个可覆盖变量, 详见 `docker-compose.yml`。

---

## 七、API 一览

基础路径 `/api/v1`, 下表省略前缀。

### 基础

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1` | 已注册模块列表 |
| GET | `/healthz` | 存活探针 |
| GET | `/readyz` | 就绪探针(含数据库 ping) |

### 路灯台账

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/lamps` | 分页查询(关键字/道路/运行状态/灯具类型) |
| POST | `/lamps` | 新增路灯 |
| GET | `/lamps/options` | 下拉选项(编号/名称/道路) |
| GET | `/lamps/statistics` | 台账统计(按道路/运行状态/灯具类型) |
| GET | `/lamps/:id` | 详情 |
| PUT | `/lamps/:id` | 修改 |
| DELETE | `/lamps/:id` | 删除(存在未闭环故障时返回 409) |

### 故障登记

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/faults` | 分页查询(状态/等级/类型/道路/时间区间) |
| POST | `/faults` | 登记故障(同一路灯存在未闭环故障时返回 409) |
| GET | `/faults/meta` | 字典: 类型/等级/来源/状态 |
| GET | `/faults/:id` | 详情 |
| PUT | `/faults/:id` | 修改 |
| DELETE | `/faults/:id` | 删除(仅已关闭且无维修记录) |
| POST | `/faults/:id/close` | 关闭故障 |

### 维修记录录入

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/repairs` | 分页查询(状态/结果/维修人/班组) |
| POST | `/repairs` | 录入维修记录(即开工; 同一故障已有进行中记录时返回 409) |
| GET | `/repairs/meta` | 字典: 状态/结果 |
| GET | `/repairs/statistics` | 维修统计(次数/平均耗时/费用) |
| GET | `/repairs/fault/:faultId` | 某故障下的全部维修记录 |
| GET | `/repairs/:id` | 详情 |
| PUT | `/repairs/:id` | 修改 |
| POST | `/repairs/:id/finish` | 完工(写入完工时间与维修结果) |
| DELETE | `/repairs/:id` | 删除(会重算故障的维修次数; 已结算月份记录拒绝删除) |

### 维修费用结算

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/settlements` | 结算单分页查询(结算单号/班组/状态/月份) |
| POST | `/settlements` | 按班组+完工月份建账(自动归集, 金额由服务端计算) |
| GET | `/settlements/meta` | 字典: 状态/动作 + 可建账班组与月份 |
| GET | `/settlements/ledger` | 费用台账: 已完工维修记录及其结算/锁定状态 + 汇总 |
| GET | `/settlements/preview` | 建账前预览归集明细(不落库) |
| GET | `/settlements/:id` | 详情: 当前版本明细 + 流转记录 + 实时归集差异标记 |
| DELETE | `/settlements/:id` | 删除(仅草稿/已驳回) |
| POST | `/settlements/:id/submit` | 提交审核(草稿 → 待审核) |
| POST | `/settlements/:id/reject` | 驳回(必须填写驳回原因) |
| POST | `/settlements/:id/resubmit` | 驳回后重新提交(重新归集, 生成新版本快照) |
| POST | `/settlements/:id/approve` | 审核通过(月份锁定) |
| GET | `/settlements/:id/diff` | 版本差异对比, 参数 `from_version` / `to_version` |

### 维修状态查询

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/status/overview` | 全局概览: 台账/故障/维修汇总 + 类型与道路分布 + 逾期清单 |
| GET | `/status/lamps` | 路灯级维修状态列表(可只看未闭环) |
| GET | `/status/track` | 全链路追踪, 参数 `fault_no` 或 `lamp_code` |

---

## 八、业务规则

- **单号**: 故障单 `GD + YYYYMMDD + 4 位序号`, 维修单 `WX + YYYYMMDD + 4 位序号`, 唯一索引冲突时自动重试。
- **一灯一单**: 同一路灯同时只允许一条未闭环故障(409); 同一故障同时只允许一条进行中维修记录(409)。
- **故障状态机**: `待处理 -> 维修中 -> 已修复 -> 已关闭`; 已关闭的故障拒绝任何再操作; 允许 `已修复 -> 维修中` 回退。
- **维修结果**: 只有 `已修复(fixed)` 会把故障推进到 `已修复`, 其余结果保持 `维修中`。
- **路灯运行状态自动联动**: 存在 `维修中` 故障 -> `维修中(maintenance)`; 存在 `待处理` 故障 -> `故障(fault)`; 否则 -> `正常(normal)`; `停用(offline)` 由人工维护。
- **删除维修记录** 会重算故障的 `repair_count` / `latest_repair_id`, 次数归零时把故障从 `维修中` 回退到 `待处理`。
- **删除路灯** 会被未闭环故障拦截; **删除故障** 要求已关闭且没有维修记录。
- **逾期判定**: 上报超过 24 小时仍未闭环的故障计入概览的逾期清单。

### 费用结算规则

- **归集口径**: 按「维修班组 + 完工时间所在月(YYYY-MM)」归集该班组当月**已完工**维修记录; 结算金额由服务端实时汇总 `cost` 得到, 前端不可填写, 保证结算金额与维修记录费用合计一致。
- **唯一约束**: 同一班组同一月份只允许一张结算单(数据库联合唯一索引), 该月份无已完工记录时不允许建账。
- **状态机**: `草稿(draft) -> 待审核(submitted) -> 已通过(approved) / 已驳回(rejected)`; 驳回后只能「重新提交」(再次进入待审核), 已通过为终态; 草稿与已驳回可以删除, 待审核/已通过不可删除。
- **流转留痕**: 建账/提交/驳回/重提/通过每一步都写入流水(`settlement_flow`), 记录操作人、原因、当时版本、条数与金额; **驳回必须填写原因**。
- **版本快照与差异**: 每次提交都把当时明细固化为一个版本(`settlement_item`), 驳回重提生成新版本而不覆盖历史; 版本对比接口逐条标出新增/移除/费用变更及金额差。
- **月份锁定**: 结算单一旦「待审核」, 该班组该月维修记录即不允许完工入账或删除(返回 409); 「已通过」后永久锁定; 驳回后解锁以便修正维修记录。该约束通过 `repair.SetSettlementLock` 反向端口实现。

---

## 九、演示数据

`APP_SEED=true`(默认)且库中还没有台账数据时, 首次启动会写入一组演示数据, 打开页面即可看到效果:

- 6 条道路, 每条 5 盏路灯, 共 **30 盏**, 覆盖 `正常 / 故障 / 维修中 / 停用` 四种运行状态;
- **19 条故障**, 覆盖待处理 / 维修中 / 已修复 / 已关闭四种状态, 其中包含 2 条已逾期;
- **17 条维修记录**, 含进行中与已完工, 带耗材、费用与维修结果;
- **3 张费用结算单**: 上月二班(驳回 → 调整维修记录班组 → 重提 → 通过, 含 2 个版本与差异)、上月一班(已通过, 月份已锁定)、本月一班(待审核)。

想从空库开始, 把 `APP_SEED` 设为 `false`, 或删除数据卷后重启。
