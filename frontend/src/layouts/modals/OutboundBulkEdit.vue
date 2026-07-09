<template>
  <v-dialog transition="dialog-bottom-transition" width="900">
    <v-card class="rounded-lg" :loading="loading">
      <v-card-title>
        {{ $t('bulk.editOutbounds') }}（{{ $t('selected') }} {{ items.length }}）
      </v-card-title>
      <v-divider></v-divider>

      <!-- 编辑表单 -->
      <v-card-text v-if="!showPreview" style="padding: 0 16px;">
        <div class="text-medium-emphasis pa-2">{{ $t('bulk.editHint') }}</div>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-checkbox v-model="fields.server" :label="$t('in.addr')" hide-details density="compact" />
            <v-text-field v-model="values.server" :disabled="!fields.server" hide-details density="compact" variant="outlined" class="mt-2" />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-checkbox v-model="fields.server_port" :label="$t('in.port')" hide-details density="compact" />
            <v-text-field v-model.number="values.server_port" type="number" :disabled="!fields.server_port" hide-details density="compact" variant="outlined" class="mt-2" />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-checkbox v-model="fields.username" :label="$t('user')" hide-details density="compact" />
            <v-text-field v-model="values.username" :disabled="!fields.username" hide-details density="compact" variant="outlined" class="mt-2" />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-checkbox v-model="fields.password" :label="$t('password')" hide-details density="compact" />
            <v-text-field v-model="values.password" :disabled="!fields.password" hide-details density="compact" variant="outlined" class="mt-2" />
          </v-col>
        </v-row>
      </v-card-text>

      <!-- 预览表格 -->
      <v-card-text v-else style="padding: 0 16px;">
        <v-data-table
          :headers="previewHeaders"
          :items="previewItems"
          :items-per-page="-1"
          hide-default-footer
          density="compact"
          class="elevation-1 rounded"
        >
          <template v-slot:item.change="{ item }">
            <span v-for="(c, i) in item.changes" :key="i" class="d-block">
              <span class="text-medium-emphasis">{{ c.label }}:</span>
              <span dir="ltr" class="mx-1">{{ c.old }}</span>
              <v-icon size="small" icon="mdi-arrow-right" />
              <span dir="ltr" class="ml-1 font-weight-bold" :class="c.new !== c.old ? 'text-primary' : ''">{{ c.new }}</span>
            </span>
            <span v-if="item.changes.length === 0" class="text-medium-emphasis">{{ $t('noChange') }}</span>
          </template>
        </v-data-table>
      </v-card-text>

      <v-card-actions>
        <v-spacer></v-spacer>
        <v-btn color="primary" variant="outlined" @click="$emit('close')">{{ $t('actions.close') }}</v-btn>
        <template v-if="!showPreview">
          <v-btn color="primary" variant="tonal" :disabled="!hasFieldSelected" @click="buildPreview">{{ $t('preview') }}</v-btn>
        </template>
        <template v-else>
          <v-btn color="secondary" variant="outlined" @click="showPreview = false">{{ $t('back') }}</v-btn>
          <v-btn color="primary" variant="tonal" :loading="loading" @click="execute">{{ $t('actions.save') }}</v-btn>
        </template>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { ref, computed, watch } from 'vue'
import { i18n } from '@/locales'
import Data from '@/store/modules/data'
import { push } from 'notivue'

const props = defineProps<{
  visible: boolean
  items: any[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const loading = ref(false)
const showPreview = ref(false)

const fields = ref({
  server: false,
  server_port: false,
  username: false,
  password: false,
})

const values = ref({
  server: '',
  server_port: 0,
  username: '',
  password: '',
})

const hasFieldSelected = computed(() => Object.values(fields.value).some(v => v))

const fieldLabels: Record<string, string> = {
  server: i18n.global.t('in.addr'),
  server_port: i18n.global.t('in.port'),
  username: i18n.global.t('user'),
  password: i18n.global.t('password'),
}

const previewHeaders = computed(() => [
  { title: i18n.global.t('objects.tag'), key: 'tag' },
  { title: i18n.global.t('type'), key: 'type' },
  { title: i18n.global.t('changes'), key: 'change', sortable: false },
])

const previewItems = ref<any[]>([])

function buildPreview() {
  previewItems.value = props.items.map((item: any) => {
    const changes: { label: string; old: any; new: any }[] = []
    for (const key of Object.keys(fields.value)) {
      if ((fields.value as any)[key]) {
        changes.push({
          label: fieldLabels[key],
          old: item[key] ?? '-',
          new: (values.value as any)[key],
        })
      }
    }
    return { tag: item.tag, type: item.type, changes, _raw: item }
  })
  showPreview.value = true
}

async function execute() {
  loading.value = true
  const payload = props.items.map((item: any) => {
    const updated = { ...item }
    for (const key of Object.keys(fields.value)) {
      if ((fields.value as any)[key]) {
        updated[key] = (values.value as any)[key]
      }
    }
    return updated
  })
  const success = await Data().save('outbounds', 'editbulk', payload)
  loading.value = false
  if (success) {
    push.success(i18n.global.t('bulk.editSuccess', { count: payload.length }))
    emit('close')
  }
}

watch(() => props.visible, (v: boolean) => {
  if (v) {
    showPreview.value = false
    fields.value = { server: false, server_port: false, username: false, password: false }
    values.value = { server: '', server_port: 0, username: '', password: '' }
  }
})
</script>
