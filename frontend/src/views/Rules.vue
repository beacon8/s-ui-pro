<template>
  <RuleVue
    v-model="ruleModal.visible"
    :visible="ruleModal.visible"
    :index="ruleModal.index"
    :data="ruleModal.data"
    :clients="clients"
    :inTags="inboundTags"
    :outTags="outboundTags"
    :rsTags="rulesetTags"
    @close="closeRuleModal"
    @save="saveRuleModal"
  />
  <RulesetVue
    v-model="rulesetModal.visible"
    :visible="rulesetModal.visible"
    :index="rulesetModal.index"
    :data="rulesetModal.data"
    :outTags="outboundTags"
    @close="closeRulesetModal"
    @save="saveRulesetModal"
  />
  <RuleImport
    v-model="importRulesModal.visible"
    :visible="importRulesModal.visible"
    :existingRulesCount="rules.length"
    :existingRulesetsCount="rulesets.length"
    :existingRulesetTags="rulesetTags"
    :importing="importing"
    @save="saveImportRule"
    @close="closeImportRule"
  />
  <RulesetImport
    v-model="importRulesetsModal.visible"
    :visible="importRulesetsModal.visible"
    :outTags="outboundTags"
    :rsTags="rulesetTags"
    @save="saveImportRulesets"
    @close="closeImportRulesets"
  />
  <v-row>
    <v-col cols="12" justify="center" align="center">
      <v-btn color="primary" @click="showRuleModal(-1)" style="margin: 0 5px;">{{ $t('rule.add') }}</v-btn>
      <v-btn color="primary" @click="showRulesetModal(-1)" style="margin: 0 5px;">{{ $t('ruleset.add') }}</v-btn>
      <v-menu v-model="actionMenu" :close-on-content-click="false" location="bottom center">
        <template v-slot:activator="{ props }">
          <v-btn v-bind="props" hide-details variant="text" icon>
            <v-icon icon="mdi-tools" color="primary" />
          </v-btn>
        </template>
        <v-list density="compact" nav>
          <v-list-item link @click="showImportRule">
            <template v-slot:prepend>
              <v-icon icon="mdi-routes"></v-icon>
            </template>
            <v-list-item-title v-text="$t('rule.import.rulesTitle')"></v-list-item-title>
          </v-list-item>
          <v-list-item link @click="showImportRulesets">
            <template v-slot:prepend>
              <v-icon icon="mdi-download-multiple"></v-icon>
            </template>
            <v-list-item-title v-text="$t('rule.import.title')"></v-list-item-title>
          </v-list-item>
        </v-list>
      </v-menu>
      <v-btn variant="outlined" color="warning" @click="saveConfig" :loading="loading" :disabled="stateChange">
        {{ $t('actions.save') }}
      </v-btn>
    </v-col>
  </v-row>
  <v-row>
    <v-col class="v-card-subtitle" cols="12">{{ $t('basic.routing.title') }}</v-col>
    <v-col cols="12">
      <v-row>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-select hide-details :label="$t('basic.routing.defaultOut')" clearable
            @click:clear="delete route.final" :items="outboundTags" v-model="route.final"></v-select>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field v-model="route.default_interface" hide-details clearable
            @click:clear="delete route.default_interface" :label="$t('basic.routing.defaultIf')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-text-field v-model.number="routeMark" hide-details type="number" min="0" :label="$t('basic.routing.defaultRm')"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6" md="3" lg="2">
          <v-switch v-model="route.auto_detect_interface" color="primary" :label="$t('basic.routing.autoBind')" hide-details></v-switch>
        </v-col>
      </v-row>
    </v-col>
  </v-row>
  <v-row>
    <v-col class="v-card-subtitle" cols="12">{{ $t('rule.ruleset') }}</v-col>
    <v-col cols="12">
      <v-data-table
        :headers="rulesetHeaders"
        :items="<any[]>rulesets"
        :items-per-page="itemPerPage"
        @update:items-per-page="setItemPerPage($event)"
        :hide-default-footer="rulesets.length<=10"
        hide-no-data
        fixed-header
        item-value="tag"
        :mobile="smAndDown"
        mobile-breakpoint="sm"
        width="100%"
        class="elevation-3 rounded"
      >
        <template v-slot:item.actions="{ item }">
          <v-icon class="me-2" @click="showRulesetModal(rulesets.findIndex(r => r.tag == item.tag))">mdi-file-edit</v-icon>
          <v-menu
            v-model="delRulesetOverlay[rulesets.findIndex(r => r.tag == item.tag)]"
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
                <v-btn color="error" variant="outlined" @click="delRuleset(rulesets.findIndex(r => r.tag == item.tag))">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delRulesetOverlay[rulesets.findIndex(r => r.tag == item.tag)] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-menu>
        </template>
        <template v-slot:item.type="{ item }">{{ $t('ruleset.' + item.type) }}</template>
        <template v-slot:item.download_detour="{ item }">{{ item.download_detour ?? '-' }}</template>
        <template v-slot:item.update_interval="{ item }">{{ item.update_interval ?? '-' }}</template>
      </v-data-table>
    </v-col>
  </v-row>
  <v-row>
    <v-col class="v-card-subtitle" cols="12">{{ $t('pages.rules') }}</v-col>
    <v-col cols="12" sm="4" md="3">
      <v-text-field
        v-model="searchRuleOut"
        :label="$t('search') + ' (' + $t('objects.outbound') + ')'"
        prepend-inner-icon="mdi-magnify"
        clearable
        hide-details
        density="compact"
        variant="outlined"
      ></v-text-field>
    </v-col>
    <v-col cols="12">
      <v-data-table
        :headers="ruleHeaders"
        :items="<any[]>displayRules"
        :items-per-page="itemPerPage"
        @update:items-per-page="setItemPerPage($event)"
        :hide-default-footer="displayRules.length<=10"
        hide-no-data
        fixed-header
        item-value="_idx"
        :mobile="smAndDown"
        mobile-breakpoint="sm"
        width="100%"
        class="elevation-3 rounded"
      >
        <template v-slot:item.order="{ item }">
          <span
            :draggable="true"
            @dragstart="onDragStart(item._idx)" @dragover.prevent @drop="onDrop(item._idx)"
            style="cursor: move;"
          >
            <v-icon size="small" icon="mdi-drag" /> {{ item._idx + 1 }}
          </span>
        </template>
        <template v-slot:item.actions="{ item }">
          <v-icon class="me-2" @click="showRuleModal(item._idx)">mdi-file-edit</v-icon>
          <v-menu
            v-model="delRuleOverlay[item._idx]"
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
                <v-btn color="error" variant="outlined" @click="delRule(item._idx)">{{ $t('yes') }}</v-btn>
                <v-btn color="success" variant="outlined" @click="delRuleOverlay[item._idx] = false">{{ $t('no') }}</v-btn>
              </v-card-actions>
            </v-card>
          </v-menu>
        </template>
        <template v-slot:item.kind="{ item }">
          {{ item.type != undefined ? $t('rule.logical') + ' (' + item.mode + ')' : $t('rule.simple') }}
        </template>
        <template v-slot:item.action="{ item }">{{ item.action }}</template>
        <template v-slot:item.outbound="{ item }">{{ item.outbound ?? '-' }}</template>
        <template v-slot:item.count="{ item }">
          {{ item.rules ? item.rules.length : Object.keys(item).filter(r => !actionKeys.includes(r) && r != '_idx').length }}
        </template>
        <template v-slot:item.invert="{ item }">{{ $t((item.invert ?? false) ? 'yes' : 'no') }}</template>
      </v-data-table>
    </v-col>
  </v-row>
</template>

<script lang="ts" setup>
import Data from '@/store/modules/data'
import { computed, ref, onBeforeMount } from 'vue'
import RuleVue from '@/layouts/modals/Rule.vue'
import RulesetVue from '@/layouts/modals/Ruleset.vue'
import RulesetImport from '@/layouts/modals/RulesetImport.vue'
import RuleImport from '@/layouts/modals/RuleImport.vue'
import { Config } from '@/types/config'
import { actionKeys, ruleset } from '@/types/rules'
import { FindDiff } from '@/plugins/utils'
import { push } from 'notivue'
import { useI18n } from 'vue-i18n'
import { useDisplay } from 'vuetify'
import { i18n } from '@/locales'

const { smAndDown } = useDisplay()

const oldConfig = ref({})
const loading = ref(false)
const actionMenu = ref(false)
const importing = ref(false)
const { t } = useI18n()
const appConfig = computed((): Config => {
  return <Config> Data().config
})

onBeforeMount(async () => {
  loading.value = true
  while (Data().lastLoad == 0) {
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  loading.value = false
})

const routeMark = computed({
  get() { return route.value.default_mark ?? 0 },
  set(v:number) { v>0 ? route.value.default_mark = v : delete appConfig.value.route.default_mark }
})

const stateChange = computed(() => FindDiff.deepCompare(appConfig.value, oldConfig.value))

const saveConfig = async () => {
  loading.value = true
  const success = await Data().save("config", "set", appConfig.value)
  if (success) {
    oldConfig.value = JSON.parse(JSON.stringify(Data().config))
    loading.value = false
  }
}

const clients = computed((): string[] => Data().clients.map((c:any) => c.name))
const route = computed((): any => appConfig.value.route ?? {})

const rules = computed((): any[] => {
  const data = route.value
  if (!data) return []
  if (!('rules' in data) || !Array.isArray(data.rules)) data.rules = []
  return data.rules
})

const rulesets = computed((): any[] => {
  const data = route.value
  if (!data) return []
  if (!('rule_set' in data) || !Array.isArray(data.rule_set)) data.rule_set = []
  return data.rule_set
})

const rulesetTags = computed((): string[] => rulesets.value.map((rs:any) => rs.tag))

const searchRuleOut = ref('')

// 给每条规则打上全局索引 _idx，再按出站(outbound)模糊过滤；表格自带分页
const displayRules = computed(() => {
  const list = rules.value.map((item:any, index:number) => ({ ...item, _idx: index }))
  const q = searchRuleOut.value?.trim().toLowerCase()
  if (!q) return list
  return list.filter((r:any) => (r.outbound ?? '').toLowerCase().includes(q))
})

const itemPerPage = ref(localStorage.getItem('items-per-page') || '10')
const setItemPerPage = (items: number) => {
  itemPerPage.value = items.toString()
  localStorage.setItem('items-per-page', items.toString())
}

const ruleHeaders = [
  { title: i18n.global.t('actions.action'), key: 'actions', sortable: false },
  { title: '#', key: 'order', sortable: false },
  { title: i18n.global.t('type'), key: 'kind', sortable: false },
  { title: i18n.global.t('admin.action'), key: 'action' },
  { title: i18n.global.t('objects.outbound'), key: 'outbound' },
  { title: i18n.global.t('pages.rules'), key: 'count', sortable: false },
  { title: i18n.global.t('rule.invert'), key: 'invert', sortable: false },
]

const rulesetHeaders = [
  { title: i18n.global.t('actions.action'), key: 'actions', sortable: false },
  { title: i18n.global.t('objects.tag'), key: 'tag' },
  { title: i18n.global.t('type'), key: 'type' },
  { title: i18n.global.t('ruleset.format'), key: 'format' },
  { title: i18n.global.t('objects.outbound'), key: 'download_detour' },
  { title: i18n.global.t('actions.update'), key: 'update_interval' },
]

const outboundTags = computed((): string[] => [
  ...Data().outbounds?.map((o:any) => o.tag),
  ...Data().endpoints?.map((e:any) => e.tag)
])

const inboundTags = computed((): string[] => [
  ...Data().inbounds?.map((o:any) => o.tag),
  ...Data().endpoints?.filter((e:any) => e.listen_port > 0).map((e:any) => e.tag)
])

let delRuleOverlay = ref(new Array<boolean>)
let delRulesetOverlay = ref(new Array<boolean>)

const ruleModal = ref({ visible: false, index: -1, data: "" })
const showRuleModal = (index: number) => {
  ruleModal.value.index = index
  ruleModal.value.data = index == -1 ? '' : JSON.stringify(rules.value[index])
  ruleModal.value.visible = true
}
const closeRuleModal = () => { ruleModal.value.visible = false }
const saveRuleModal = (data:any) => {
  if (ruleModal.value.index == -1) rules.value.push(data)
  else rules.value[ruleModal.value.index] = data
  ruleModal.value.visible = false
}
const delRule = (index: number) => { rules.value.splice(index, 1); delRuleOverlay.value[index] = false }

const rulesetModal = ref({ visible: false, index: -1, data: "" })
const showRulesetModal = (index: number) => {
  rulesetModal.value.index = index
  rulesetModal.value.data = index == -1 ? '' : JSON.stringify(rulesets.value[index])
  rulesetModal.value.visible = true
}
const closeRulesetModal = () => { rulesetModal.value.visible = false }
const saveRulesetModal = (data:ruleset) => {
  if (rulesetModal.value.index == -1) rulesets.value.push(data)
  else rulesets.value[rulesetModal.value.index] = data
  rulesetModal.value.visible = false
}
const delRuleset = (index: number) => { rulesets.value.splice(index, 1); delRulesetOverlay.value[index] = false }

const draggedItemIndex = ref(null)
const onDragStart = (index: any) => { draggedItemIndex.value = index }
const onDrop = (index: any) => {
  if (draggedItemIndex.value !== null) {
    const draggedItem = rules.value[draggedItemIndex.value]
    rules.value.splice(draggedItemIndex.value, 1)
    rules.value.splice(index, 0, draggedItem)
    draggedItemIndex.value = null
  }
}

const importRulesModal = ref({ visible: false })

function showImportRule() {
  importRulesModal.value.visible = true
}

function closeImportRule() {
  importRulesModal.value.visible = false
}

// Collect user/auth_user from a rule recursively (handles logical rules)
function collectRuleUsers(r: any): Set<string> {
  const users = new Set<string>()
  const pick = (arr: any[]) => { if (Array.isArray(arr)) arr.forEach((u: string) => u && users.add(u)) }
  pick(r.user)
  pick(r.auth_user)
  if (r.type === 'logical' && Array.isArray(r.rules)) {
    r.rules.forEach((sub: any) => collectRuleUsers(sub).forEach((u) => users.add(u)))
  }
  return users
}

// Build occupied set from existing rules (only action===route or action absent)
function buildOccupiedUsers(existingRules: any[]): Set<string> {
  const occupied = new Set<string>()
  for (const r of existingRules) {
    const act = r.action
    if (act && act !== 'route') continue
    collectRuleUsers(r).forEach((u) => occupied.add(u))
  }
  return occupied
}

async function saveImportRule(block: any, mode: 'merge' | 'replace', applyFinal: boolean) {
  if (mode === 'replace') {
    route.value.rules = block.rules ?? []
    route.value.rule_set = block.rule_set ?? []
    if (applyFinal && block.final) route.value.final = block.final
    importing.value = true
    const ok = await Data().save('config', 'set', appConfig.value)
    importing.value = false
    if (ok) {
      oldConfig.value = JSON.parse(JSON.stringify(Data().config))
      importRulesModal.value.visible = false
    }
    return
  }

  // merge mode: conflict detection
  const occupied = buildOccupiedUsers(rules.value)
  let added = 0
  let skipped = 0

  const existingTags = new Set(rulesetTags.value)
  const incomingRules: any[] = block.rules ?? []
  const incomingRulesets: any[] = block.rule_set ?? []

  for (const r of incomingRules) {
    const users = collectRuleUsers(r)
    if (users.size > 0) {
      const hasConflict = [...users].some((u) => occupied.has(u))
      if (hasConflict) { skipped++; continue }
      // Add this rule's users to occupied so same-batch duplicates are caught
      users.forEach((u) => occupied.add(u))
    }
    rules.value.push(r)
    added++
  }

  let addedRulesets = 0
  for (const rs of incomingRulesets) {
    if (!existingTags.has(rs.tag)) {
      rulesets.value.push(rs)
      existingTags.add(rs.tag)
      addedRulesets++
    }
  }

  if (applyFinal && block.final) route.value.final = block.final

  const totalIncoming = incomingRules.length

  // All skipped — no write, no restart
  if (added === 0 && addedRulesets === 0 && !(applyFinal && block.final)) {
    push.warning({
      title: t('rule.import.rulesTitle'),
      duration: 5000,
      message: t('rule.import.importAllSkipped', { count: totalIncoming }),
    })
    importRulesModal.value.visible = false
    return
  }

  importing.value = true
  const ok = await Data().save('config', 'set', appConfig.value)
  importing.value = false

  if (ok) {
    oldConfig.value = JSON.parse(JSON.stringify(Data().config))
    importRulesModal.value.visible = false
    if (skipped > 0) {
      push.success({
        title: t('rule.import.rulesTitle'),
        duration: 5000,
        message: t('rule.import.importPartial', { added, skipped }),
      })
    } else {
      push.success({
        title: t('rule.import.rulesTitle'),
        duration: 5000,
        message: t('rule.import.importSuccess', { count: added }),
      })
    }
  }
}

const importRulesetsModal = ref({ visible: false })

function showImportRulesets() {
  importRulesetsModal.value.visible = true
}

function closeImportRulesets() {
  importRulesetsModal.value.visible = false
}

function saveImportRulesets(items: any[]) {
  rulesets.value.push(...items)
  importRulesetsModal.value.visible = false
}
</script>
