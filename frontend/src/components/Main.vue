<template>
  <LogVue v-model="logModal.visible" :control="logModal" :visible="logModal.visible" />
  <Backup v-model="backupModal.visible" :control="backupModal" :visible="backupModal.visible" />
  <UsageStats v-model:visible="usageStatsModal.visible" />
  <v-container class="fill-height" :loading="loading">
    <v-responsive :class="reloadItems.length>0 ? 'fill-height text-center' : 'align-center'" >
      <v-row class="d-flex align-center justify-center">
        <v-col cols="auto">
          <v-img src="@/assets/logo.svg" :width="reloadItems.length>0 ? 100 : 200"></v-img>
        </v-col>
      </v-row>
      <v-row class="d-flex align-center justify-center">
        <v-col cols="auto">
          <div class="d-flex flex-wrap align-center justify-center" style="gap: 10px;">
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
        </v-col>
      </v-row>
      <v-row style="gap: 16px;">
        <v-col cols="12" sm="6" md="4" lg="3" v-for="i in reloadItems" :key="i" style="min-width: 280px;">
          <v-card class="rounded-lg" variant="outlined" style="min-height: 210px;" elevation="5">
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
            <v-card-text style="padding: 12px 16px;" align="center" justify="center">
              <div v-if="i.charAt(0) == 'g'" style="height: 150px; display: flex; align-items: center; justify-content: center;">
                <Gauge :tilesData="tilesData" :type="i" />
              </div>
              <div v-if="i.charAt(0) == 'h'" style="height: 150px; position: relative;">
                <History :tilesData="tilesData" :type="i" />
              </div>
              <template v-if="i == 'i-sys'">
                <div class="info-row" v-for="row in [
                  { label: $t('main.info.host'), value: tilesData.sys?.hostName },
                  { label: $t('main.info.cpu'), chip: true, chipText: (tilesData.sys?.cpuCount ?? '') + ' ' + $t('main.info.core'), tooltip: tilesData.sys?.cpuType },
                  { label: 'IP', ips: [{ text: 'IPv4', val: tilesData.sys?.ipv4 }, { text: 'IPv6', val: tilesData.sys?.ipv6 }] },
                  { label: 'S-UI', chip: true, chipText: 'v' + (tilesData.sys?.appVersion ?? ''), chipColor: 'blue' },
                  { label: $t('main.info.uptime'), value: HumanReadable.formatSecond((Date.now()/1000) - tilesData.sys?.bootTime), tooltip: $t('main.info.startupTime') + ': ' + new Date((tilesData.sys?.bootTime || 0) * 1000).toLocaleString(locale) },
                ]">
                  <span class="info-label">{{ row.label }}</span>
                  <span class="info-value">
                    <template v-if="row.value">{{ row.value }}</template>
                    <v-chip density="compact" variant="flat" v-else-if="row.chip" :color="row.chipColor">
                      <v-tooltip v-if="row.tooltip" activator="parent" location="top" style="direction: ltr;">{{ row.tooltip }}</v-tooltip>
                      {{ row.chipText }}
                    </v-chip>
                    <template v-else-if="row.ips">
                      <template v-for="ip in row.ips" :key="ip.text">
                        <v-chip density="compact" color="primary" variant="flat" v-if="ip.val?.length>0" style="margin-inline-end: 6px;">
                          <v-tooltip activator="parent" location="top" style="direction: ltr;">
                            <span v-html="ip.val?.join('<br />')"></span>
                          </v-tooltip>
                          {{ ip.text }}
                        </v-chip>
                      </template>
                    </template>
                  </span>
                </div>
              </template>
              <template v-if="i == 'i-sbd'">
                <div class="info-row" v-for="row in [
                  { label: $t('main.info.running'), sbdRunning: tilesData.sbd?.running },
                  { label: $t('main.info.memory'), chip: true, chipText: HumanReadable.sizeFormat(tilesData.sbd?.stats?.Alloc), chipColor: 'primary', show: tilesData.sbd?.stats?.Alloc },
                  { label: $t('main.info.threads'), chip: true, chipText: tilesData.sbd?.stats?.NumGoroutine, chipColor: 'primary', show: tilesData.sbd?.stats?.NumGoroutine },
                  { label: $t('main.info.uptime'), value: HumanReadable.formatSecond(tilesData.sbd?.stats?.Uptime) },
                  { label: $t('online'), onlines: Data().onlines, sbdRunning: tilesData.sbd?.running },
                ]">
                  <span class="info-label">{{ row.label }}</span>
                  <span class="info-value">
                    <template v-if="row.value">{{ row.value }}</template>
                    <template v-else-if="row.sbdRunning !== undefined && !row.onlines">
                      <v-chip density="compact" color="success" variant="flat" v-if="row.sbdRunning">{{ $t('yes') }}</v-chip> 
                      <v-chip density="compact" color="error" variant="flat" v-else>{{ $t('no') }}</v-chip>
                      <v-chip density="compact" color="transparent" v-if="row.sbdRunning && !loading" style="cursor: pointer; margin-inline-start: 6px;" @click="restartSingbox()">
                        <v-tooltip activator="parent" location="top">{{ $t('actions.restartSb') }}</v-tooltip>
                        <v-icon icon="mdi-restart" color="warning" />
                      </v-chip>
                    </template>
                    <v-chip density="compact" variant="flat" v-else-if="row.chip && row.show" :color="row.chipColor">{{ row.chipText }}</v-chip>
                    <template v-else-if="row.onlines && row.sbdRunning">
                      <v-chip density="compact" color="primary" variant="flat" v-if="row.onlines.user" style="margin-inline-end: 6px;">
                        <v-tooltip activator="parent" location="top" overflow="auto">
                          <span v-text="$t('pages.clients')" style="font-weight: bold;"></span><br/>
                          <span v-for="user in row.onlines.user">{{ user }}<br /></span>
                        </v-tooltip>
                        {{ row.onlines.user?.length }}
                      </v-chip>
                      <v-chip density="compact" color="success" variant="flat" v-if="row.onlines.inbound" style="margin-inline-end: 6px;">
                        <v-tooltip activator="parent" location="top" :text="$t('pages.inbounds')">
                          <span v-text="$t('pages.inbounds')" style="font-weight: bold;"></span><br/>
                          <span v-for="i in row.onlines.inbound">{{ i }}<br /></span>
                        </v-tooltip>
                        {{ row.onlines.inbound?.length }}
                      </v-chip>
                      <v-chip density="compact" color="info" variant="flat" v-if="row.onlines.outbound">
                        <v-tooltip activator="parent" location="top" :text="$t('pages.outbounds')">
                          <span v-text="$t('pages.outbounds')" style="font-weight: bold;"></span><br/>
                          <span v-for="o in row.onlines.outbound">{{ o }}<br /></span>
                        </v-tooltip>
                        {{ row.onlines.outbound?.length }}
                      </v-chip>
                    </template>
                  </span>
                </div>
              </template>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>
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
.info-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 4px 0;
  border-bottom: 1px solid #f0f0f0;
  font-size: 13px;
  text-align: left;
}
.info-row:last-child {
  border-bottom: none;
}
.info-label {
  flex-shrink: 0;
  color: rgba(0,0,0,0.65);
  white-space: nowrap;
}
.info-value {
  flex: 1;
  text-align: right;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 4px;
}
</style>
