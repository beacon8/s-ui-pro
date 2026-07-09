<template>
  <v-dialog transition="dialog-bottom-transition" width="900">
    <v-card class="rounded-lg" :loading="loading">
      <v-card-title>
        {{ $t('bulk.editOutbounds') }}（{{ $t('selected') }} {{ items.length }}）
      </v-card-title>
      <v-divider></v-divider>

      <!-- 多行输入模式 -->
      <v-card-text v-if="!showPreview" style="padding: 0 16px;">
        <div class="text-medium-emphasis pa-2">
          {{ $t('bulk.editHint') }}
        </div>
        <v-textarea
          v-model="batchText"
          :label="$t('bulk.batchInput')"
          :placeholder="placeholder"
          rows="10"
          variant="outlined"
          hide-details
          dir="ltr"
          class="pa-2"
        />
        <div class="text-medium-emphasis px-2 pb-2" v-if="parseError">
          <v-alert :text="parseError" type="error" variant="outlined" density="compact" />
        </div>
        <div class="text-medium-emphasis px-2 pb-2" v-else>
          <span v-if="parsedLines.length > 0">{{ $t('bulk.parsedCount', { count: parsedLines.length }) }} / {{ items.length }}</span>
        </div>
      </v-card-text>

      <!-- 预览表格 -->
      <v-card-text v-else style="padding: 0 16px; max-height: 55vh; overflow-y: auto;">
        <v-data-table
          :headers="previewHeaders"
          :items="previewItems"
          :items-per-page="-1"
          hide-default-footer
          density="compact"
          class="elevation-1 rounded"
          fixed-header
        >
          <template v-slot:item.server="{ item }">
            <span dir="ltr">{{ item.oldServer }}</span>
            <v-icon size="small" icon="mdi-arrow-right" class="mx-1" />
            <span dir="ltr" class="text-primary font-weight-bold">{{ item.newServer }}</span>
          </template>
          <template v-slot:item.server_port="{ item }">
            <span dir="ltr">{{ item.oldPort }}</span>
            <v-icon size="small" icon="mdi-arrow-right" class="mx-1" />
            <span dir="ltr" class="text-primary font-weight-bold">{{ item.newPort }}</span>
          </template>
          <template v-slot:item.username="{ item }">
            <span dir="ltr">{{ item.oldUser || '-' }}</span>
            <v-icon size="small" icon="mdi-arrow-right" class="mx-1" />
            <span dir="ltr" class="text-primary font-weight-bold">{{ item.newUser || '-' }}</span>
          </template>
          <template v-slot:item.password="{ item }">
            <span dir="ltr">{{ maskPwd(item.oldPwd) }}</span>
            <v-icon size="small" icon="mdi-arrow-right" class="mx-1" />
            <span dir="ltr" class="text-primary font-weight-bold">{{ maskPwd(item.newPwd) }}</span>
          </template>
        </v-data-table>
      </v-card-text>

      <v-card-actions>
        <v-spacer></v-spacer>
        <v-btn color="primary" variant="outlined" @click="$emit('close')">{{ $t('actions.close') }}</v-btn>
        <template v-if="!showPreview">
          <v-btn color="primary" variant="tonal" :disabled="!canPreview" @click="buildPreview">{{ $t('preview') }}</v-btn>
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
const batchText = ref('')

const placeholder = '1.2.3.4:1080:user1:pass1\n5.6.7.8:1080:user2:pass2:with:colons'

// 解析多行文本，每行 ip:port:username:password
const parsedLines = computed(() => {
  const lines = batchText.value.split('\n').map(l => l.trim()).filter(l => l.length > 0)
  const result: { server: string; server_port: number; username: string; password: string }[] = []
  for (const line of lines) {
    const parts = line.split(':')
    if (parts.length < 4) continue
    const server = parts[0]
    const port = parseInt(parts[1], 10)
    const username = parts[2]
    const password = parts.slice(3).join(':')
    if (!server || isNaN(port)) continue
    result.push({ server, server_port: port, username, password })
  }
  return result
})

const parseError = computed(() => {
  const lines = batchText.value.split('\n').map(l => l.trim()).filter(l => l.length > 0)
  if (lines.length === 0) return ''
  if (lines.length !== parsedLines.value.length) {
    return i18n.global.t('bulk.parseError', { bad: lines.length - parsedLines.value.length })
  }
  if (parsedLines.value.length !== props.items.length) {
    return i18n.global.t('bulk.countMismatch', { lines: parsedLines.value.length, items: props.items.length })
  }
  return ''
})

const canPreview = computed(() => {
  if (parsedLines.value.length === 0) return false
  if (parseError.value) return false
  return true
})

const previewHeaders = computed(() => [
  { title: i18n.global.t('objects.tag'), key: 'tag' },
  { title: i18n.global.t('in.addr'), key: 'server' },
  { title: i18n.global.t('in.port'), key: 'server_port' },
  { title: i18n.global.t('user'), key: 'username' },
  { title: i18n.global.t('password'), key: 'password' },
])

const previewItems = ref<any[]>([])

function maskPwd(pwd: string): string {
  if (!pwd) return '-'
  if (pwd.length <= 4) return '****'
  return pwd.slice(0, 2) + '****' + pwd.slice(-2)
}

function buildPreview() {
  previewItems.value = props.items.map((item: any, i: number) => {
    const p = parsedLines.value[i]
    return {
      tag: item.tag,
      oldServer: item.server ?? '-',
      newServer: p.server,
      oldPort: item.server_port ?? '-',
      newPort: p.server_port,
      oldUser: item.username ?? '',
      newUser: p.username,
      oldPwd: item.password ?? '',
      newPwd: p.password,
    }
  })
  showPreview.value = true
}

async function execute() {
  loading.value = true
  const payload = props.items.map((item: any, i: number) => {
    const p = parsedLines.value[i]
    return { ...item, server: p.server, server_port: p.server_port, username: p.username, password: p.password }
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
    batchText.value = ''
  }
})
</script>
