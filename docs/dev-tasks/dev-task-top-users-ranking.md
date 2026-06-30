# 开发任务：用户流量排行 API + Clients 页排行视图

> **目标读者**：实施此任务的 AI agent。
> **本文档原则**：所有结论已基于源码确认；agent 无需再做仓库探索。按本文逐项实施即可。
> **不允许的动作**：执行 `git commit/push`；改动数据库 schema（`database/db.go` 的 `AutoMigrate` 列表 / `database/model/model.go`）；改动 sing-box 内核启动链路；改动 `sub/`、`core/`、`cronjob/` 任何文件；改动 `service/stats.go` 中既有的 `SaveStats / GetStats / GetOnlines / DelOldStats / downsampleStats` 方法。

---

## 0. 进度速览

| 模块 | 状态 | 文件 |
|---|---|---|
| 后端 service | **已完成** | `service/stats.go` |
| 后端 API handler | **已完成** | `api/apiService.go` |
| 后端路由 `/api/topUsers` | **已完成** | `api/apiHandler.go` |
| 后端路由 `/apiv2/topUsers` | **已完成** | `api/apiV2Handler.go` |
| 前端模态框组件 | **待完成** | `frontend/src/layouts/modals/TopUsers.vue`（新建） |
| 前端 Clients 页入口 | **待完成** | `frontend/src/views/Clients.vue` |
| i18n 6 语言 key | **待完成** | `frontend/src/locales/{en,fa,vi,zhcn,zhtw,ru}.ts` |
| 编译验证 | **待完成** | `go build` + `npm run build` |

Agent 拿到本仓库后，先 `git diff` 查看后端已应用改动，确认与第 4 节一致，然后做第 5–7 节剩余工作即可。

---

## 1. 背景与目标

S-UI 现已支持「单个用户的流量历史曲线」（Clients 页柱状图按钮调起 `frontend/src/layouts/modals/Stats.vue` 拉 `GET /api/stats`）。本次新增「按用户做流量排行」能力，**优先目标是对外提供 API**，附带在 Clients 页加一个排行视图入口。

### 1.1 核心需求

1. 新增 `GET /api/topUsers`（cookie session）与 `GET /apiv2/topUsers`（Header `Token: xxx`）两个接口，返回 Top N 客户端流量。
2. 同时支持「累计排行」（直接读 `clients` 表）与「时段排行」（聚合 `stats` 表）。
3. 排序方向可切换：合计 / 仅上行 / 仅下行（服务端排序，调用方不需要二次处理）。
4. **被禁用客户端也参与排行**（不过滤 `enable`）。
5. Clients 页提供一个图标按钮触发模态框，**界面参数固定**（period=24h、direction=both、limit=10），用横向柱状图展示。
6. `stats` 表保留天数沿用现有 setting `trafficAge`，建议运维设为 30，**接口本身不读/不改 `trafficAge`**。

### 1.2 不需要改数据库

- `clients` 表已有 `up / down / total_up / total_down` 字段（`database/model/model.go:34-45`）→ 「累计」直接 `ORDER BY` 即可。
- `stats` 表已是时间序列 `(date_time, resource, tag, direction, traffic)`（`database/model/model.go:53-60`）→ 「时段」用 `SUM(traffic) GROUP BY tag, direction` 聚合即可。
- `direction` 字段语义：`true=上行(up)`，`false=下行(down)`。对照 `service/stats.go:60-69` `SaveStats` 实现确认。

---

## 2. API 契约

### 2.1 路由

| 方法 | 路径 | 鉴权 | 适用调用方 |
|---|---|---|---|
| GET | `/api/topUsers` | Cookie session（浏览器登录） | 面板前端 |
| GET | `/apiv2/topUsers` | Header `Token: <api token>` | 脚本 / 第三方 |

> 两条路由复用同一个 service 方法 `StatsService.GetTopUsers`。Token 在面板「管理员 → API Token」处生成。

### 2.2 Query 参数

| 参数 | 取值 | 默认 | 行为 |
|---|---|---|---|
| `period` | `total` / `1h` / `24h` / `7d` / `30d` | `total` | `total` → 读 `clients` 表（累计至今）；其余 → 聚合 `stats` 表 `WHERE date_time > now - <span>` |
| `direction` | `both` / `up` / `down` | `both` | 决定服务端按哪个字段降序排序：`both → total`、`up → up`、`down → down` |
| `limit` | 整数 1..100 | `10` | 返回条数上限；越界自动夹紧到 `[1, 100]`；非数字回落到 10 |

非法 `period` / `direction` 返回 `{success:false, msg:"invalid period: xxx"}`。

### 2.3 响应

成功：

```json
{
  "success": true,
  "msg": "",
  "obj": [
    { "name": "alice", "up": 12345678, "down": 87654321, "total": 100000000 },
    { "name": "bob",   "up":   123456, "down":  4567890, "total":   4691346 }
  ]
}
```

字段含义：

- `name`：`clients.name`（即 sing-box 用户 tag）。
- `up`：上行字节（period=total 时为 `clients.up` 当前周期累计；period=时段时为该窗口内 `SUM(traffic) WHERE direction=true`）。
- `down`：下行字节，同上。
- `total`：`up + down`，服务端预算好。

**注意 period=total 的口径**：用的是 `clients.up / clients.down`，这两个字段在「周期重置」时会被归零（`AutoReset + ResetDays + NextReset` 机制，见 `service/clients.go` 的 `ResetClients`）。如果调用方希望看「自创建以来的真累计」，应改读 `total_up / total_down`——本任务不暴露该模式，需求方按需后续再扩字段。

### 2.4 调用示例

```bash
# Cookie session（浏览器已登录态）
curl -b 's-ui=...' 'http://127.0.0.1:2095/app/api/topUsers?period=24h&limit=10'

# API Token（脚本/第三方）
curl -H 'Token: <your-token>' \
     'http://127.0.0.1:2095/app/apiv2/topUsers?period=7d&direction=down&limit=20'
```

> 路径前缀 `/app/` 由 setting `webPath` 决定，默认 `/app/`。

---

## 3. 数据流与算法

```
┌────────────────┐  period=total  ┌────────────────┐
│ GET topUsers   │ ──────────────▶│  clients 表     │── ORDER BY <sortBy> DESC LIMIT N
│  ?period=...   │                └────────────────┘
│  ?direction=.. │
│  ?limit=...    │  period=时段    ┌────────────────┐
└────────────────┘ ──────────────▶│  stats 表       │── SUM(traffic) GROUP BY tag, direction
                                  └────────────────┘     ↓ Go 内存聚合 ↓
                                                       map[tag] → {up, down, total}
                                                       sort.Slice 按 sortBy 降序
                                                       slice 截断到 limit
```

`sortBy` 映射规则：

| direction 入参 | sortBy（SQL ORDER BY / Go sort key） |
|---|---|
| `both` 或缺省 | `total`（= up+down） |
| `up` | `up` |
| `down` | `down` |

period=时段时不在 SQL 层做 ORDER BY（因为先要在 Go 里把 `(tag, direction)` 两行合并成一行 `{up, down, total}`），改用 `sort.Slice` 在内存排序后切片。

---

## 4. 后端实施细节（已应用，agent 复核即可）

### 4.1 `service/stats.go`

**(a) import 增加 `util/common`：**

```go
import (
    "sort"
    "time"

    "github.com/admin8800/s-ui/database"
    "github.com/admin8800/s-ui/database/model"
    "github.com/admin8800/s-ui/util/common"

    "gorm.io/gorm"
)
```

**(b) 在 `GetOnlines` 方法之后追加 `TopUser` 类型与 `GetTopUsers` 方法：**

```go
// TopUser 流量排行单条记录
type TopUser struct {
    Name  string `json:"name"`
    Up    int64  `json:"up"`
    Down  int64  `json:"down"`
    Total int64  `json:"total"`
}

// GetTopUsers 按流量返回 Top N 客户端
//
//   period: total / 1h / 24h / 7d / 30d
//   direction: both / up / down（决定排序字段）
//   limit: 1..100，默认 10
//
// 不过滤 enable，停用客户端也参与排行。
func (s *StatsService) GetTopUsers(period string, limit int, direction string) ([]TopUser, error) {
    if limit <= 0 {
        limit = 10
    }
    if limit > 100 {
        limit = 100
    }

    sortBy := "total"
    switch direction {
    case "up":
        sortBy = "up"
    case "down":
        sortBy = "down"
    case "", "both":
        sortBy = "total"
    default:
        return nil, common.NewError("invalid direction: ", direction)
    }

    db := database.GetDB()

    // 累计：直接从 clients 表读
    if period == "" || period == "total" {
        var result []TopUser
        orderExpr := sortBy + " DESC"
        err := db.Model(&model.Client{}).
            Select("name, up, down, up+down AS total").
            Order(orderExpr).
            Limit(limit).
            Scan(&result).Error
        return result, err
    }

    // 时段：聚合 stats 表
    var since int64
    now := time.Now().Unix()
    switch period {
    case "1h":
        since = now - 3600
    case "24h":
        since = now - 86400
    case "7d":
        since = now - 7*86400
    case "30d":
        since = now - 30*86400
    default:
        return nil, common.NewError("invalid period: ", period)
    }

    type aggRow struct {
        Tag       string
        Direction bool
        Sum       int64
    }
    var rows []aggRow
    err := db.Model(&model.Stats{}).
        Select("tag, direction, SUM(traffic) AS sum").
        Where("resource = ? AND date_time > ?", "user", since).
        Group("tag").
        Group("direction").
        Scan(&rows).Error
    if err != nil {
        return nil, err
    }

    agg := make(map[string]*TopUser, len(rows))
    for _, r := range rows {
        u, ok := agg[r.Tag]
        if !ok {
            u = &TopUser{Name: r.Tag}
            agg[r.Tag] = u
        }
        if r.Direction {
            u.Up += r.Sum
        } else {
            u.Down += r.Sum
        }
    }

    result := make([]TopUser, 0, len(agg))
    for _, u := range agg {
        u.Total = u.Up + u.Down
        result = append(result, *u)
    }

    sort.Slice(result, func(i, j int) bool {
        switch sortBy {
        case "up":
            return result[i].Up > result[j].Up
        case "down":
            return result[i].Down > result[j].Down
        default:
            return result[i].Total > result[j].Total
        }
    })

    if len(result) > limit {
        result = result[:limit]
    }
    return result, nil
}
```

### 4.2 `api/apiService.go`

**在 `GetStatus` 方法上方插入新 handler：**

```go
func (a *ApiService) GetTopUsers(c *gin.Context) {
    period := c.Query("period")
    direction := c.Query("direction")
    limit, err := strconv.Atoi(c.Query("limit"))
    if err != nil {
        limit = 10
    }
    data, err := a.StatsService.GetTopUsers(period, limit, direction)
    if err != nil {
        jsonMsg(c, "", err)
        return
    }
    jsonObj(c, data, nil)
}
```

### 4.3 `api/apiHandler.go` 与 `api/apiV2Handler.go`

两个文件的 `getHandler` switch 中，`"stats"` 分支之后加：

```go
    case "topUsers":
        a.ApiService.GetTopUsers(c)
```

> 这两条变更后端均已应用。Agent 仅需 `git diff` 复核与本节文本一致。

---

## 5. 前端实施（待完成）

### 5.1 新建 `frontend/src/layouts/modals/TopUsers.vue`

**职责**：弹窗展示 Top 10 用户的横向柱状图。固定 `period=24h`、`direction=both`、`limit=10`，**不暴露切换控件**（按需求"界面固定式"）。

**参考样式**：复用 `frontend/src/layouts/modals/Stats.vue` 的 `v-dialog` 外壳与 `chart.js` 注册套路；只是把 `Line` 换成 `Bar`，并加 `BarElement` 注册。

**完整骨架（agent 直接落盘）**：

```vue
<template>
  <v-dialog transition="dialog-bottom-transition" width="800">
    <v-card class="rounded-lg" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col cols="auto">{{ $t('stats.topUsers') }}</v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto"><v-icon icon="mdi-close" @click="$emit('close')"></v-icon></v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text style="padding: 0 16px;">
        <div style="text-align: center; margin: 5px;">
          {{ $t('stats.topUsersSubtitle') }}
        </div>
        <v-container id="container" style="height:50vh;">
          <v-skeleton-loader class="mx-auto border" width="95%" type="image" v-if="loading"></v-skeleton-loader>
          <template v-else>
            <v-alert :text="$t('noData')" type="warning" variant="outlined" v-if="alert"></v-alert>
            <Bar v-if="loaded" :data="chartData" :options="<any>options" />
          </template>
        </v-container>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { HumanReadable } from '@/plugins/utils'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
} from 'chart.js'
import { ref } from 'vue'
import { Bar } from 'vue-chartjs'

ChartJS.register(CategoryScale, LinearScale, BarElement, Title, Tooltip, Legend)
ChartJS.defaults.font.family = 'Vazirmatn'

export default {
  components: { Bar },
  props: ['visible'],
  data() {
    return {
      loading: false,
      loaded: false,
      alert: false,
      options: {
        indexAxis: 'y',
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { display: true },
          tooltip: {
            callbacks: {
              label: (ctx: any) => {
                const v = ctx.parsed.x
                return ctx.dataset.label + ': ' + HumanReadable.sizeFormat(v)
              },
            },
          },
        },
        scales: {
          x: {
            beginAtZero: true,
            ticks: {
              callback: (v: any) => (v == 0 ? 0 : HumanReadable.sizeFormat(v, 0)),
            },
          },
        },
      },
      chartData: ref(<any>{}),
    }
  },
  methods: {
    async loadData() {
      this.loading = true
      const data = await HttpUtils.get('api/topUsers', {
        period: '24h',
        direction: 'both',
        limit: 10,
      })
      if (data.success && data.obj && (<any[]>data.obj).length > 0) {
        const obj = <any[]>data.obj
        const labels = obj.map(o => o.name)
        const ups = obj.map(o => o.up)
        const downs = obj.map(o => o.down)
        this.chartData = {
          labels,
          datasets: [
            {
              label: i18n.global.t('stats.upload'),
              backgroundColor: 'rgba(255, 165, 0, 0.6)',
              borderColor: 'rgba(255, 165, 0, 1)',
              borderWidth: 1,
              data: ups,
            },
            {
              label: i18n.global.t('stats.download'),
              backgroundColor: 'rgba(0, 128, 0, 0.4)',
              borderColor: 'rgba(0, 128, 0, 1)',
              borderWidth: 1,
              data: downs,
            },
          ],
        }
        this.loaded = true
        this.alert = false
      } else {
        this.alert = true
        this.loaded = false
      }
      this.loading = false
    },
  },
  watch: {
    visible(v) {
      if (v) {
        this.loadData()
      } else {
        this.loaded = false
        this.alert = false
      }
    },
  },
}
</script>
```

**实现要点**：

- `Bar` 来自 `vue-chartjs`，**必须显式注册 `BarElement`**，否则图不会渲染。
- `indexAxis: 'y'` 让柱状图变横向（用户名在 Y 轴）。
- 堆叠不必要：上行/下行用两组分组柱子并列即可（同一 Y label 两条柱）。如果产品想要叠加，可改 `options.scales` 加 `stacked: true`。
- HTTP 路径与 `Stats.vue` 一致用相对路径 `api/topUsers`（`httputil` 会自动拼 `webBasePath`）。
- 无需 `setInterval` 轮询（固定窗口、用户体感不需要实时刷新）；关闭弹窗自动清状态。

### 5.2 修改 `frontend/src/views/Clients.vue`

在工具栏（现有 actionMenu / filterMenu 同行）追加一个奖杯图标按钮，触发 TopUsers 弹窗。

**(a) `<template>` 中，在 `filterMenu` 的 `</v-col>` 之后插入：**

```vue
    <v-col cols="auto">
      <v-btn variant="text" icon @click="showTopUsers" v-if="Data().enableTraffic">
        <v-icon icon="mdi-trophy" color="primary" />
        <v-tooltip activator="parent" location="top" :text="$t('stats.topUsers')"></v-tooltip>
      </v-btn>
    </v-col>
```

**(b) `<template>` 顶部，与 `<Stats>` 平级追加：**

```vue
  <TopUsers
    v-model="topUsersModal"
    :visible="topUsersModal"
    @close="closeTopUsers"
  />
```

**(c) `<script setup>` 中 import 与 ref：**

```ts
import TopUsers from '@/layouts/modals/TopUsers.vue'

// ...其余 ref 之后
const topUsersModal = ref(false)
const showTopUsers = () => { topUsersModal.value = true }
const closeTopUsers = () => { topUsersModal.value = false }
```

**实现要点**：

- 按钮用 `v-if="Data().enableTraffic"` 做开关：如果运维把 `trafficAge` 设为 0（不存 stats），按钮不显示，避免用户点出来全空。
- 不要去动 `headers` / `v-data-table`：本任务**只在工具栏加入口**，不修改表格列。
- 不要在 `Clients.vue` 加新的轮询；TopUsers 组件自己按需取数。

### 5.3 chart.js Bar 依赖

`frontend/package.json` 中 `chart.js` 与 `vue-chartjs` 已存在（Stats.vue 已在用）。**无需 `npm install`**，只需在 `TopUsers.vue` 中 `ChartJS.register(..., BarElement, ...)`。

---

## 6. i18n（待完成）

在 `frontend/src/locales/` 下 6 个文件 `en.ts / fa.ts / vi.ts / zhcn.ts / zhtw.ts / ru.ts` 的 `stats` 对象内追加两个 key：

| key | en | zhcn | zhtw | fa | vi | ru |
|---|---|---|---|---|---|---|
| `stats.topUsers` | Top Users | 流量排行 | 流量排行 | برترین کاربران | Top người dùng | Топ пользователей |
| `stats.topUsersSubtitle` | Top 10 by traffic in last 24h | 近 24 小时流量 Top 10 | 近 24 小時流量 Top 10 | ۱۰ کاربر برتر ۲۴ ساعت اخیر | Top 10 trong 24h qua | Топ 10 за последние 24 часа |

> 翻译仅供参考，agent 可让 native speaker 校对。若拿不准，先用英文兜底 + 中文，其他 4 语言留 TODO 不阻塞编译（vue-i18n 在缺 key 时会回落到 fallback locale 而非报错）。

---

## 7. 验证清单（必须全过）

执行顺序：

1. **后端编译**

   ```bash
   go build -tags "with_quic with_grpc with_utls with_acme with_gvisor with_naive_outbound with_tailscale" -o /tmp/sui-build-test main.go
   ```

   预期：无错误（warning 可忽略）。

2. **前端编译**

   ```bash
   cd frontend && npm run build
   ```

   预期：vue-tsc 与 vite 都成功，无 missing key 报错（i18n 警告可忽略）。

3. **接口手测**（启动 `./sui` 后）

   ```bash
   # 登录拿 cookie（替换密码）
   curl -c /tmp/c.txt -X POST -d 'user=admin&pass=admin' http://127.0.0.1:2095/app/api/login

   # 累计排行
   curl -b /tmp/c.txt 'http://127.0.0.1:2095/app/api/topUsers'
   # 24h，仅按下行排序
   curl -b /tmp/c.txt 'http://127.0.0.1:2095/app/api/topUsers?period=24h&direction=down&limit=5'
   # 非法 period
   curl -b /tmp/c.txt 'http://127.0.0.1:2095/app/api/topUsers?period=xxx'
   # → 预期 {"success":false,"msg":"invalid period: xxx"}
   ```

4. **apiv2 手测**（先在面板创建 token）

   ```bash
   curl -H 'Token: <token>' 'http://127.0.0.1:2095/app/apiv2/topUsers?period=total&limit=20'
   ```

5. **前端 UI**

   - 浏览器打开 Clients 页 → 工具栏看到 trophy 图标
   - 点击 → 弹窗显示 Top 10 横向柱状图
   - 弹窗内每条用户有「上行 / 下行」两条并列柱子
   - 若数据库无 stats 记录，弹窗显示 `noData` 警告（不是白屏）

---

## 8. 已知约束与提示

1. **`stats` 表无索引**。当前 schema（`database/model/model.go:53-60`）只有自增主键。当 `trafficAge=30` 且活跃用户/连接较多时，`SUM ... GROUP BY tag, direction WHERE date_time > ?` 在大表上可能慢。**本任务不加索引**（属于跨任务的存量优化），如运维反馈慢，后续单独立任务加 `(resource, date_time)` 联合索引。
2. **`direction` 字段语义**：`true=up`、`false=down`。判定来源是 `service/stats.go:60-69` 的 `SaveStats`：`if stat.Direction { up += } else { down += }`。
3. **period=total 的实际口径**是「当前重置周期内 + 启用排行后累积」，因为 `clients.up / down` 在 `ResetClients`（`service/clients.go`）按 `ResetDays` 归零。若需「真历史累计」，将来扩 `period=lifetime` 改读 `total_up / total_down` 即可。
4. **`trafficAge` 的运维设置**：在面板「设置」中改成 30（单位天），即 stats 保留 30 天。本接口**不会**读这个值，也**不会**改它；它只决定 stats 表能聚合多久的窗口。如果运维设 7，那么 `period=30d` 实际只有 7 天数据。
5. **不要做的事**：
   - 不要把 `topUsers` 加入 `apiv2Handler.go` 的 POST 分支（只有 GET）。
   - 不要在 Clients 页加任何"刷新间隔"配置；当前轮询由 `router/index.ts` 的 10 秒 `Data().loadData()` 统一负责。
   - 不要新加数据库表/列。

---

## 9. 文件清单（agent 落地时对照）

后端（已应用，仅复核）：

- `service/stats.go`
- `api/apiService.go`
- `api/apiHandler.go`
- `api/apiV2Handler.go`

前端（待新建/修改）：

- `frontend/src/layouts/modals/TopUsers.vue`（新建）
- `frontend/src/views/Clients.vue`（修改）
- `frontend/src/locales/en.ts`
- `frontend/src/locales/fa.ts`
- `frontend/src/locales/vi.ts`
- `frontend/src/locales/zhcn.ts`
- `frontend/src/locales/zhtw.ts`
- `frontend/src/locales/ru.ts`

文档：

- 本文件 `docs/dev-task-top-users-ranking.md`
