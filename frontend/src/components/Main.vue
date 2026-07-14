<template>
  <LogVue v-model="logModal.visible" :control="logModal" :visible="logModal.visible" />
  <Backup v-model="backupModal.visible" :control="backupModal" :visible="backupModal.visible" />
  <UsageStats v-model:visible="usageStatsModal.visible" />
  <v-container class="fill-height" :loading="loading">
    <v-responsive :class="reloadItems.length>0 ? 'fill-height text-center' : 'align-center'" >
    <div class="dashboard-header">
      <div class="logo-row">
        <v-img src="@/assets/logo.svg" :width="reloadItems.length>0 ? 100 : 200"></v-img>
      </div>
      <div class="btn-row">
          <v-dialog v-model="menu" :close-on-content-click="false" transition="scale-transition" max-width="800">
            <template v-slot:activator="{ props }">
              <v-btn v-bind="props" hide-details variant="tonal" elevation="3">{{ $t('main.tiles') }} <v-icon icon="mdi-star-plus" /></v-btn>
            </template>
            <v-card rounded="xl">
              <v-card-title>
                <v-row>
                  <v-col>
                    {{ $t('main.tiles') }}
                  </v-col>
                  <v-spacer></v-spacer>
                  <v-col cols="auto"><v-icon icon="mdi-close" @click="menu = false"></v-icon></v-col>
                </v-row>
              </v-card-title>
              <v-divider></v-divider>
              <v-row v-for="items in menuItems" density="compact">
                <v-col cols="12">
                  <v-card :subtitle="items.title" variant="flat">
                    <v-card-text>
                      <v-row density="compact">
                        <v-col cols="12" md="6" lg="3" v-for="item in items.value">
                          <v-switch
                          density="compact"
                          v-model="reloadItems"
                          :value="item.value"
                          color="primary"
                          :label="item.title"
                          hide-details></v-switch>
                        </v-col>
                      </v-row>
                    </v-card-text>
                  </v-card>
                </v-col>
              </v-row>
            </v-card>
          </v-dialog>
          <v-btn variant="tonal" hide-details elevation="3"
            @click="backupModal.visible = true">{{ $t('main.backup.title') }}<v-icon icon="mdi-backup-restore" />
          </v-btn>
          <v-btn variant="tonal" hide-details elevation="3"
            @click="logModal.visible = true">{{ $t('basic.log.title') }} <v-icon icon="mdi-list-box-outline" />
          </v-btn>
          <v-btn variant="tonal" hide-details elevation="3"
            @click="usageStatsModal.visible = true">{{ $t('main.stats.title') }} <v-icon icon="mdi-chart-box-outline" />
          </v-btn>
      </div>
    </div>
    <div class="dashboard-grid">
        <v-card class="rounded-lg" variant="outlined" style="min-height: 210px;" elevation="5" v-for="i in reloadItems" :key="i">
            <v-card-title>
              {{ menuItems.flatMap(cat => cat.value).find(m => m.value == i)?.title }}
              <template v-if="i == 'i-sys'">
                <v-icon icon="mdi-update" color="primary"
                  @click="reloadSys()" size="small" v-tooltip:top="$t('actions.update')"
                  style="margin-inline-start: 10px;">
                </v-icon>
              </template>
              <template v-if="i == 'h-net'">
                <v-icon icon="mdi-information" color="primary" size="small"
                  v-tooltip:top="'↓' + 
                  HumanReadable.sizeFormat(tilesData.net?.recv) + ' - ' + 
                  HumanReadable.sizeFormat(tilesData.net?.sent) + '↑'"
                  style="margin-inline-start: 10px;">
                </v-icon>
              </template>
            </v-card-title>
            <v-card-text class="dash-card-body" align="center" justify="center">
              <div v-if="i.charAt(0) == 'g'" class="gauge-wrap">
                <Gauge :tilesData="tilesData" :type="i" />
              </div>
              <div v-if="i.charAt(0) == 'h'" class="chart-wrap">
                <History :tilesData="tilesData" :type="i" />
              </div>
              <template v-if="i == 'i-sys'">
                <v-row class="info-grid">
                  <v-col cols="4" class="info-label">{{ $t('main.info.host') }}</v-col>
                  <v-col cols="8" class="info-value" style="text-wrap: nowrap; overflow: hidden; text-overflow: ellipsis;">{{ tilesData.sys?.hostName }}</v-col>
                  <v-col cols="4" class="info-label">{{ $t('main.info.cpu') }}</v-col>
                  <v-col cols="8" class="info-value">
                    <v-chip density="compact" variant="flat">
                      <v-tooltip activator="parent" location="top" style="direction: ltr;">
                        {{ tilesData.sys?.cpuType }}
                      </v-tooltip>
                     {{ tilesData.sys?.cpuCount }} {{ $t('main.info.core') }}
                    </v-chip>
                  </v-col>
                  <v-col cols="4" class="info-label">IP</v-col>
                  <v-col cols="8" class="info-value">
                    <v-chip density="compact" color="primary" variant="flat" v-if="tilesData.sys?.ipv4?.length>0">
                      <v-tooltip activator="parent" location="top" style="direction: ltr;">
                        <span v-html="tilesData.sys?.ipv4?.join('<br />')"></span>
                      </v-tooltip>
                      IPv4
                    </v-chip>
                    <v-chip density="compact" color="primary" variant="flat" v-if="tilesData.sys?.ipv6?.length>0">
                      <v-tooltip activator="parent" location="top" style="direction: ltr;">
                        <span v-html="tilesData.sys?.ipv6?.join('<br />')"></span>
                      </v-tooltip>
                      IPv6
                    </v-chip>
                  </v-col>
                  <v-col cols="4" class="info-label">S-UI</v-col>
                  <v-col cols="8" class="info-value">
                    <v-chip density="compact" color="blue">
                      v{{ tilesData.sys?.appVersion }}
                    </v-chip>
                  </v-col>
                  <v-col cols="4" class="info-label">{{ $t('main.info.uptime') }}</v-col>
                  <v-col cols="8" class="info-value" v-tooltip:top="$t('main.info.startupTime')
                    + ': ' + new Date((tilesData.sys?.bootTime || 0) * 1000).toLocaleString(locale)">
                    {{ HumanReadable.formatSecond((Date.now()/1000) - tilesData.sys?.bootTime) }}
                  </v-col>
                </v-row>
              </template>
              <template v-if="i == 'i-sbd'">
                <v-row class="info-grid">
                  <v-col cols="4" class="info-label">{{ $t('main.info.running') }}</v-col>
                  <v-col cols="8" class="info-value">
                    <v-chip density="compact" color="success" variant="flat" v-if="tilesData.sbd?.running">{{ $t('yes') }}</v-chip> 
                    <v-chip density="compact" color="error" variant="flat" v-else>{{ $t('no') }}</v-chip>
                    <v-chip density="compact" color="transparent" v-if="tilesData.sbd?.running && !loading" style="cursor: pointer;" @click="restartSingbox()">
                      <v-tooltip activator="parent" location="top">
                        {{ $t('actions.restartSb') }}
                      </v-tooltip>
                      <v-icon icon="mdi-restart" color="warning" />
                    </v-chip>
                  </v-col>
                  <v-col cols="4" class="info-label">{{ $t('main.info.memory') }}</v-col>
                  <v-col cols="8" class="info-value">
                    <v-chip density="compact" color="primary" variant="flat" v-if="tilesData.sbd?.stats?.Alloc">
                      {{ HumanReadable.sizeFormat(tilesData.sbd?.stats?.Alloc) }}
                    </v-chip> 
                  </v-col>
                  <v-col cols="4" class="info-label">{{ $t('main.info.threads') }}</v-col>
                  <v-col cols="8" class="info-value">
                    <v-chip density="compact" color="primary" variant="flat" v-if="tilesData.sbd?.stats?.NumGoroutine">
                      {{ tilesData.sbd?.stats?.NumGoroutine }}
                    </v-chip>
                  </v-col>
                  <v-col cols="4" class="info-label">{{ $t('main.info.uptime') }}</v-col>
                  <v-col cols="8" class="info-value">{{ HumanReadable.formatSecond(tilesData.sbd?.stats?.Uptime) }}</v-col>
                  <v-col cols="4" class="info-label">{{ $t('online') }}</v-col>
                  <v-col cols="8" class="info-value">
                    <template v-if="tilesData.sbd?.running">
                      <v-chip density="compact" color="primary" variant="flat" v-if="Data().onlines.user">
                        <v-tooltip activator="parent" location="top" overflow="auto">
                          <span v-text="$t('pages.clients')" style="font-weight: bold;"></span><br/>
                          <span v-for="user in Data().onlines.user">{{ user }}<br /></span>
                        </v-tooltip>
                        {{ Data().onlines.user?.length }}
                      </v-chip>
                      <v-chip density="compact" color="success" variant="flat" v-if="Data().onlines.inbound">
                        <v-tooltip activator="parent" location="top" :text="$t('pages.inbounds')">
                          <span v-text="$t('pages.inbounds')" style="font-weight: bold;"></span><br/>
                          <span v-for="i in Data().onlines.inbound">{{ i }}<br /></span>
                        </v-tooltip>
                        {{ Data().onlines.inbound?.length }}
                      </v-chip>
                      <v-chip density="compact" color="info" variant="flat" v-if="Data().onlines.outbound">
                        <v-tooltip activator="parent" location="top" :text="$t('pages.outbounds')">
                          <span v-text="$t('pages.outbounds')" style="font-weight: bold;"></span><br/>
                          <span v-for="o in Data().onlines.outbound">{{ o }}<br /></span>
                        </v-tooltip>
                        {{ Data().onlines.outbound?.length }}
                      </v-chip>
                    </template>
                  </v-col>
                </v-row>
              </template>
            </v-card-text>
          </v-card>
    </div>
     </v-responsive>
   </v-container>
</template>

<script lang="ts" setup>
import HttpUtils from '@/plugins/httputil'
import { HumanReadable } from '@/plugins/utils'
import Data from '@/store/modules/data'
import Gauge from '@/components/tiles/Gauge.vue'
import History from '@/components/tiles/History.vue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { i18n, locale } from '@/locales'
import LogVue from '@/layouts/modals/Logs.vue'
import Backup from '@/layouts/modals/Backup.vue'
import UsageStats from '@/layouts/modals/UsageStats.vue'

const loading = ref(false)
const menu = ref(false)
const menuItems = [
  { title: i18n.global.t('main.gauges'), value: [
    { title: i18n.global.t('main.gauge.cpu'), value: "g-cpu" },
    { title: i18n.global.t('main.gauge.mem'), value: "g-mem" },
    { title: i18n.global.t('main.gauge.dsk'), value: "g-dsk" },
    { title: i18n.global.t('main.gauge.swp'), value: "g-swp" },
    ]
  },
  { title: i18n.global.t('main.charts'), value: [
    { title: i18n.global.t('main.chart.cpu'), value: "h-cpu" },
    { title: i18n.global.t('main.chart.mem'), value: "h-mem" },
    { title: i18n.global.t('main.chart.net'), value: "h-net" },
    { title: i18n.global.t('main.chart.pnet'), value: "hp-net" },
    { title: i18n.global.t('main.chart.dio'), value: "h-dio" },
    ]
  },
  { title: i18n.global.t('main.infos'), value: [
    { title: i18n.global.t('main.info.sys'), value: "i-sys" },
    { title: i18n.global.t('main.info.sbd'), value: "i-sbd" },
    ]
  },
]

const tilesData = ref(<any>{})

const reloadItems = computed({
  get() { return Data().reloadItems },
  set(v:string[]) {
    if (Data().reloadItems.length == 0 && v.length>0) startTimer()
    if (Data().reloadItems.length > 0 && v.length == 0) stopTimer()
    Data().reloadItems = v
    v.length>0 ? localStorage.setItem("reloadItems",v.join(',')) : localStorage.removeItem("reloadItems")
  }
})

const reloadData = async () => {
  const request = [...new Set(reloadItems.value.map(r => r.split('-')[1]))]
  if (tilesData.value?.sys?.appVersion) request.filter(r => r != 'sys')
  const data = await HttpUtils.get('api/status',{ r: request.join(',')})
  if (data.success) {
    tilesData.value = data.obj
  }
}

const reloadSys = async () => {
  const data = await HttpUtils.get('api/status',{ r: 'sys'})
  if (data.success) {
    tilesData.value.sys = data.obj.sys
  }
}

let intervalId: ReturnType<typeof setInterval> | null = null

const startTimer = () => {
  intervalId = setInterval(() => {
    reloadData()
  }, 2000)
}

const stopTimer = () => {
  if (intervalId) {
    clearInterval(intervalId)
    intervalId = null
  }
}

onMounted(async () => {
  loading.value = true
  if (Data().reloadItems.length != 0) {
    await reloadData()
    startTimer()
  }
  loading.value = false
})

onBeforeUnmount(() => {
  stopTimer()
})

const logModal = ref({ visible: false })

const backupModal = ref({ visible: false })

const usageStatsModal = ref({ visible: false })

const restartSingbox = async () => {
  loading.value = true
  await HttpUtils.post('api/restartSb',{})
  loading.value = false
}
</script>

<style scoped>
/* ===== 区域1: Header — logo 单独行 + 按钮行 flex 居中 ===== */
.dashboard-header {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 16px 0;
}
.logo-row {
  display: flex;
  justify-content: center;
}
.btn-row {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  align-items: center;
  gap: 12px;
}

/* ===== 区域2: Grid — 统一三列，最后一行左对齐 ===== */
.dashboard-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
  align-items: stretch;
}
@media (max-width: 960px) {
  .dashboard-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 600px) {
  .dashboard-grid {
    grid-template-columns: 1fr;
  }
}

/* ===== 区域3: Card body padding ===== */
.dash-card-body {
  padding: 16px 24px !important;
}

/* ===== 区域5: 圆环/折线图容器 ===== */
.gauge-wrap {
  height: 150px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 8px 0;
}
.chart-wrap {
  height: 150px;
  position: relative;
  padding: 8px 12px;
}

/* ===== 区域4: 系统信息/运行信息每行 flex space-between ===== */
.info-grid {
  margin: 0;
}
.info-grid .v-col {
  padding-top: 4px;
  padding-bottom: 4px;
}
.info-label {
  color: rgba(0,0,0,0.55);
  font-size: 13px;
  text-align: left;
}
.info-value {
  text-align: right;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 13px;
}
</style>
