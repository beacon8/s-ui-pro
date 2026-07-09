<template>
  <OutboundVue 
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :data="modal.data"
    :tags="outboundTags"
    @close="closeModal"
  />
  <OutboundBulk
    v-model="bulkModal.visible"
    :visible="bulkModal.visible"
    :outboundTags="outboundTags"
    @close="closeBulkModal"
  />
  <OutboundBulkEdit
    v-model="editBulkModal.visible"
    :visible="editBulkModal.visible"
    :items="selectedItems"
    @close="closeEditBulk"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />
  <v-row justify="center" align="center">
    <v-col cols="auto">
      <v-btn color="primary" @click="showModal(0)">{{ $t('actions.add') }}</v-btn>
    </v-col>
    <v-col cols="auto">
      <v-btn color="primary" @click="showBulkModal">{{ $t('actions.addbulk') }}</v-btn>
    </v-col>
    <v-col cols="auto">
      <v-btn
        color="secondary"
        variant="outlined"
        :loading="testingAll"
        append-icon="mdi-speedometer"
        :disabled="testingAll || outbounds.length === 0"
        @click="checkAllOutbounds"
      >
        {{ $t('actions.testAll') || 'Test all' }}
      </v-btn>
    </v-col>
    <v-col cols="auto" v-if="selected.length > 0">
      <v-btn color="info" variant="tonal" append-icon="mdi-file-edit-outline" @click="showEditBulk">
        {{ $t('bulk.editOutbounds') }}（{{ selected.length }}）
      </v-btn>
    </v-col>
    <v-col cols="12" sm="4" md="3">
      <v-text-field
        v-model="searchTag"
        :label="$t('search')"
        prepend-inner-icon="mdi-magnify"
        clearable
        hide-details
        density="compact"
        variant="outlined"
      ></v-text-field>
    </v-col>
  </v-row>
  <v-row>
    <v-col cols="12">
      <v-data-table
        v-model="selected"
        :headers="headers"
        :items="<any[]>outbounds"
        :search="searchTag"
        :custom-filter="filterMulti"
        :items-per-page="itemPerPage"
        @update:items-per-page="setItemPerPage($event)"
        :hide-default-footer="outbounds.length<=10"
        hide-no-data
        fixed-header
        show-select
        item-value="tag"
        :mobile="smAndDown"
        mobile-breakpoint="sm"
        width="100%"
        class="elevation-3 rounded"
      >
        <template v-slot:item.actions="{ item }">
          <v-icon class="me-2" @click="showModal(item.id)">mdi-file-edit</v-icon>
          <v-menu
            v-model="delOverlay[outbounds.findIndex(o => o.tag == item.tag)]"
            :close-on-content-click="false"
            location="top center"
          >
            <template v-slot:activator="{ props }">
              <v-icon class="me-2" color="warning" v-bind="props">mdi-file-remove</v-icon>
            </template>
            <v-card :title="$t('actions.del')" rounded="lg">
              <v-divider></v-divider>
              <v-card-text>{{ $t('confirm') }}</v-card-text>
              <v-card-actions>
                <v-btn color="error" variant="outlined" @click="delOutbound(item.tag)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delOverlay[outbounds.findIndex(o => o.tag == item.tag)] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-menu>
          <v-icon class="me-2" icon="mdi-chart-line" @click="showStats(item.tag)" v-if="Data().enableTraffic">
            <v-tooltip activator="parent" location="top" :text="$t('stats.graphTitle')"></v-tooltip>
          </v-icon>
        </template>
        <template v-slot:item.tls="{ item }">
          {{ Object.hasOwn(item,'tls') ? $t(item.tls?.enabled ? 'enable' : 'disable') : '-' }}
        </template>
        <template v-slot:item.online="{ item }">
          <v-chip v-if="onlines.includes(item.tag)" density="comfortable" size="small" color="success" variant="flat">{{ $t('online') }}</v-chip>
          <template v-else>-</template>
        </template>
        <template v-slot:item.delay="{ item }">
          <v-progress-circular v-if="checkResults[item.tag]?.loading" indeterminate size="20" />
          <v-icon icon="mdi-speedometer" v-else @click="checkOutbound(item.tag)">
            <v-tooltip activator="parent" location="top" :text="$t('actions.test')"></v-tooltip>
          </v-icon>
          <template v-if="checkResults[item.tag]?.loading == false && checkResults[item.tag]">
            <v-chip v-if="checkResults[item.tag].success" density="compact" size="small" color="success" variant="flat">
              {{ checkResults[item.tag].data?.Delay + $t('date.ms') }}
            </v-chip>
            <v-tooltip v-else location="top" :text="checkResults[item.tag].errorMessage || $t('failed')">
              <template v-slot:activator="{ props }">
                <v-icon v-bind="props" size="small" color="error" icon="mdi-close-circle" />
              </template>
            </v-tooltip>
          </template>
        </template>
      </v-data-table>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import OutboundVue from '@/layouts/modals/Outbound.vue'
import OutboundBulk from '@/layouts/modals/OutboundBulk.vue'
import OutboundBulkEdit from '@/layouts/modals/OutboundBulkEdit.vue'
import Stats from '@/layouts/modals/Stats.vue'
import { Outbound } from '@/types/outbounds'
import { computed, ref } from 'vue'
import { useDisplay } from 'vuetify'
import { i18n } from '@/locales'

const { smAndDown } = useDisplay()

interface CheckResult {
  loading?: boolean
  success: boolean
  data?: { OK?: boolean; Delay?: number; Error?: string } | null
  errorMessage?: string
}

const checkResults = ref<Record<string, CheckResult>>({})

const checkOutbound = async (tag: string) => {
  checkResults.value = { ...checkResults.value, [tag]: { loading: true, success: false } }
  const msg = await HttpUtils.get('api/checkOutbound', { tag })
  const success = msg.success && msg.obj?.OK
  const errorMessage = success ? undefined : (msg.obj?.Error ?? msg.msg ?? '')
  checkResults.value = {
    ...checkResults.value,
    [tag]: { loading: false, success, data: msg.obj ?? null, errorMessage }
  }
}

const testingAll = ref(false)

const checkAllOutbounds = async () => {
  const list = outbounds.value
  if (list.length === 0) return
  testingAll.value = true
  try {
    await Promise.all(list.map((o) => checkOutbound(o.tag)))
  } finally {
    testingAll.value = false
  }
}

const outbounds = computed((): Outbound[] => {
  return <Outbound[]> Data().outbounds
})

const searchTag = ref('')

// 多字段模糊搜索：匹配 id/tag/server/username
const filterMulti = (_value: any, query: string, item: any): boolean => {
  if (!query) return true
  const q = query.toLowerCase()
  const raw = item?.raw ?? item
  const fields = [String(raw.id ?? ''), raw.tag ?? '', raw.server ?? '', raw.username ?? '']
  return fields.some((f) => f.toLowerCase().includes(q))
}

const selected = ref<any[]>([])
const selectedItems = computed(() => selected.value.map((s: any) => s.raw ?? s))

const editBulkModal = ref({ visible: false })
const showEditBulk = () => { editBulkModal.value.visible = true }
const closeEditBulk = () => {
  editBulkModal.value.visible = false
  selected.value = []
}

const headers = [
  { title: i18n.global.t('actions.action'), key: 'actions', sortable: false },
  { title: i18n.global.t('objects.tag'), key: 'tag' },
  { title: i18n.global.t('type'), key: 'type' },
  { title: i18n.global.t('in.addr'), key: 'server' },
  { title: i18n.global.t('in.port'), key: 'server_port' },
  { title: i18n.global.t('objects.tls'), key: 'tls', sortable: false },
  { title: i18n.global.t('online'), key: 'online', sortable: false },
  { title: i18n.global.t('out.delay'), key: 'delay', sortable: false },
]

const itemPerPage = ref(localStorage.getItem('items-per-page') || '10')
const setItemPerPage = (items: number) => {
  itemPerPage.value = items.toString()
  localStorage.setItem('items-per-page', items.toString())
}

const outboundTags = computed((): string[] => {
  return [...Data().outbounds?.map((o:Outbound) => o.tag), ...Data().endpoints?.map((e:any) => e.tag)]
})

const onlines = computed(() => {
  return Data().onlines.outbound?? []
})

const modal = ref({
  visible: false,
  id: 0,
  data: "",
})

let delOverlay = ref(new Array<boolean>)

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.data = id == 0 ? '' : JSON.stringify(outbounds.value.findLast(o => o.id == id))
  modal.value.visible = true
}

const closeModal = () => {
  modal.value.visible = false
}

const bulkModal = ref({ visible: false })

const showBulkModal = () => {
  bulkModal.value.visible = true
}

const closeBulkModal = () => {
  bulkModal.value.visible = false
}

const stats = ref({
  visible: false,
  resource: "outbound",
  tag: "",
})

const delOutbound = async (tag: string) => {
  const index = outbounds.value.findIndex(i => i.tag == tag)
  const success = await Data().save("outbounds", "del", tag)
  if (success) delOverlay.value[index] = false
}

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}
</script>