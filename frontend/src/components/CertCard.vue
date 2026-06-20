<template>
  <v-card variant="outlined" class="mb-4">
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-lock</v-icon>
      {{ $t('cert.title') }}
      <v-spacer />
      <v-chip
        v-if="certStatus"
        :color="statusColor"
        size="small"
        variant="tonal"
      >{{ statusLabel }}</v-chip>
    </v-card-title>

    <v-card-text>
      <!-- Loading state -->
      <div v-if="loading && !certStatus" class="text-center py-4">
        <v-progress-circular indeterminate color="primary" />
      </div>

      <!-- Status info -->
      <div v-else-if="certStatus">
        <!-- Has cert -->
        <div v-if="certStatus.hasCert">
          <v-row dense>
            <v-col cols="12" sm="6">
              <span class="text-medium-emphasis text-caption">{{ $t('cert.field.type') }}</span>
              <div>{{ typeLabel }}</div>
            </v-col>
            <v-col v-if="certStatus.ip" cols="12" sm="6">
              <span class="text-medium-emphasis text-caption">{{ $t('cert.field.ip') }}</span>
              <div>{{ certStatus.ip }}</div>
            </v-col>
            <v-col cols="12" sm="6">
              <span class="text-medium-emphasis text-caption">{{ $t('cert.field.expiry') }}</span>
              <div>{{ formatDate(certStatus.notAfter) }}</div>
            </v-col>
            <v-col cols="12" sm="6">
              <span class="text-medium-emphasis text-caption">{{ $t('cert.field.daysLeft') }}</span>
              <div :class="certStatus.daysLeft <= 3 ? 'text-error font-weight-bold' : ''">
                {{ certStatus.daysLeft }} {{ $t('date.d') }}
              </div>
            </v-col>
          </v-row>
        </div>

        <!-- No cert -->
        <div v-else>
          <v-alert type="info" variant="tonal" density="compact" class="mb-3">
            {{ $t('cert.hint.noHttps') }}
          </v-alert>
        </div>
      </div>

      <!-- Action buttons -->
      <v-row class="mt-3" dense>
        <v-col cols="auto">
          <v-btn
            color="primary"
            variant="tonal"
            size="small"
            :loading="issuing === 'ip'"
            :disabled="!!issuing"
            @click="openIssueIPDialog"
          >{{ $t('cert.action.issueIp') }}</v-btn>
        </v-col>
        <v-col cols="auto">
          <v-btn
            variant="tonal"
            size="small"
            :loading="issuing === 'self'"
            :disabled="!!issuing"
            @click="confirmIssueSelf"
          >{{ $t('cert.action.issueSelf') }}</v-btn>
        </v-col>
        <v-col v-if="certStatus && certStatus.mode === 'le-ip'" cols="auto">
          <v-btn
            variant="tonal"
            size="small"
            :loading="issuing === 'renew'"
            :disabled="!!issuing"
            @click="confirmRenew"
          >{{ $t('cert.action.renew') }}</v-btn>
        </v-col>
        <v-col v-if="certStatus && certStatus.hasCert" cols="auto">
          <v-btn
            color="error"
            variant="tonal"
            size="small"
            :loading="issuing === 'remove'"
            :disabled="!!issuing"
            @click="confirmRemove"
          >{{ $t('cert.action.remove') }}</v-btn>
        </v-col>
      </v-row>
    </v-card-text>

    <!-- Issue IP cert dialog -->
    <v-dialog v-model="issueIPDialog" max-width="480">
      <v-card>
        <v-card-title>{{ $t('cert.precheck.title') }}</v-card-title>
        <v-card-text>
          <div v-if="precheckLoading" class="text-center py-4">
            <v-progress-circular indeterminate color="primary" />
          </div>
          <div v-else-if="precheck">
            <v-list density="compact">
              <v-list-item>
                <template #prepend>
                  <v-icon :color="precheck.publicIp ? 'success' : 'error'">
                    {{ precheck.publicIp ? 'mdi-check-circle' : 'mdi-close-circle' }}
                  </v-icon>
                </template>
                <v-list-item-title>{{ $t('cert.precheck.publicIp') }}: {{ precheck.publicIp || $t('cert.precheck.notDetected') }}</v-list-item-title>
              </v-list-item>
              <v-list-item>
                <template #prepend>
                  <v-icon :color="precheck.port80Free ? 'success' : 'error'">
                    {{ precheck.port80Free ? 'mdi-check-circle' : 'mdi-close-circle' }}
                  </v-icon>
                </template>
                <v-list-item-title>{{ $t('cert.precheck.port80') }}: {{ precheck.port80Free ? $t('cert.precheck.free') : $t('cert.precheck.occupied') }}</v-list-item-title>
              </v-list-item>
            </v-list>
            <v-alert v-if="!precheck.ok" type="error" variant="tonal" density="compact" class="mt-2">
              {{ precheck.message }}
            </v-alert>
            <v-alert v-else type="warning" variant="tonal" density="compact" class="mt-2">
              {{ $t('cert.precheck.warning') }}
            </v-alert>
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="issueIPDialog = false">{{ $t('actions.cancel') }}</v-btn>
          <v-btn
            color="primary"
            :disabled="!precheck || !precheck.ok"
            @click="doIssueIP"
          >{{ $t('actions.confirm') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Confirm dialog -->
    <v-dialog v-model="confirmDialog" max-width="400">
      <v-card>
        <v-card-title>{{ confirmTitle }}</v-card-title>
        <v-card-text>{{ confirmText }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="confirmDialog = false">{{ $t('actions.cancel') }}</v-btn>
          <v-btn color="primary" @click="confirmAction">{{ $t('actions.confirm') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-card>
</template>

<script lang="ts" setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { push } from 'notivue'
import axios from 'axios'

const { t } = useI18n()

interface CertStatus {
  mode: string
  hasCert: boolean
  certFile: string
  keyFile: string
  subject: string
  issuer: string
  ip: string
  notBefore: string
  notAfter: string
  daysLeft: number
}

interface PrecheckResult {
  publicIp: string
  port80Free: boolean
  acmeReady: boolean
  ok: boolean
  message: string
}

const certStatus = ref<CertStatus | null>(null)
const precheck = ref<PrecheckResult | null>(null)
const loading = ref(false)
const precheckLoading = ref(false)
const issuing = ref('')

const issueIPDialog = ref(false)
const confirmDialog = ref(false)
const confirmTitle = ref('')
const confirmText = ref('')
const confirmAction = ref<() => void>(() => { /* placeholder */ })

const statusColor = computed(() => {
  if (!certStatus.value || !certStatus.value.hasCert) return 'default'
  if (certStatus.value.daysLeft <= 3) return 'error'
  return 'success'
})

const statusLabel = computed(() => {
  if (!certStatus.value) return ''
  if (!certStatus.value.hasCert) return t('cert.status.http')
  return t('cert.status.https')
})

const typeLabel = computed(() => {
  if (!certStatus.value) return ''
  const modeMap: Record<string, string> = {
    'le-ip': t('cert.type.leIp'),
    'self': t('cert.type.self'),
    'manual': t('cert.type.manual'),
    'none': t('cert.type.none'),
  }
  return modeMap[certStatus.value.mode] ?? certStatus.value.mode
})

function formatDate(dateStr: string): string {
  if (!dateStr) return ''
  return new Date(dateStr).toLocaleDateString()
}

async function fetchStatus() {
  loading.value = true
  try {
    const res = await axios.get('/api/cert_status')
    if (res.data.success) certStatus.value = res.data.obj
  } catch (e) {
    // silent
  } finally {
    loading.value = false
  }
}

async function openIssueIPDialog() {
  issueIPDialog.value = true
  precheck.value = null
  precheckLoading.value = true
  try {
    const res = await axios.post('/api/cert_precheck')
    if (res.data.success) precheck.value = res.data.obj
  } catch (e) {
    // silent
  } finally {
    precheckLoading.value = false
  }
}

async function doIssueIP() {
  issueIPDialog.value = false
  issuing.value = 'ip'
  try {
    const res = await axios.post('/api/cert_issueIp', new URLSearchParams({ force: 'false' }))
    if (res.data.success) {
      push.success({ message: t('cert.notify.issueSuccess'), duration: 5000 })
      await fetchStatus()
    } else {
      push.error({ message: res.data.msg || t('cert.notify.failed'), duration: 0 })
    }
  } catch (e: any) {
    push.error({ message: e?.response?.data?.msg || String(e), duration: 0 })
  } finally {
    issuing.value = ''
  }
}

function confirmIssueSelf() {
  confirmTitle.value = t('cert.action.issueSelf')
  confirmText.value = t('cert.confirm.issueSelf')
  confirmAction.value = doIssueSelf
  confirmDialog.value = true
}

async function doIssueSelf() {
  confirmDialog.value = false
  issuing.value = 'self'
  try {
    const res = await axios.post('/api/cert_issueSelf')
    if (res.data.success) {
      push.success({ message: t('cert.notify.issueSelfSuccess'), duration: 5000 })
      await fetchStatus()
    } else {
      push.error({ message: res.data.msg || t('cert.notify.failed'), duration: 0 })
    }
  } catch (e: any) {
    push.error({ message: e?.response?.data?.msg || String(e), duration: 0 })
  } finally {
    issuing.value = ''
  }
}

function confirmRenew() {
  confirmTitle.value = t('cert.action.renew')
  confirmText.value = t('cert.confirm.renew')
  confirmAction.value = doRenew
  confirmDialog.value = true
}

async function doRenew() {
  confirmDialog.value = false
  issuing.value = 'renew'
  try {
    const res = await axios.post('/api/cert_renew')
    if (res.data.success) {
      push.success({ message: t('cert.notify.renewSuccess'), duration: 5000 })
      await fetchStatus()
    } else {
      push.error({ message: res.data.msg || t('cert.notify.failed'), duration: 0 })
    }
  } catch (e: any) {
    push.error({ message: e?.response?.data?.msg || String(e), duration: 0 })
  } finally {
    issuing.value = ''
  }
}

function confirmRemove() {
  confirmTitle.value = t('cert.action.remove')
  confirmText.value = t('cert.confirm.remove')
  confirmAction.value = doRemove
  confirmDialog.value = true
}

async function doRemove() {
  confirmDialog.value = false
  issuing.value = 'remove'
  try {
    const res = await axios.post('/api/cert_remove')
    if (res.data.success) {
      push.success({ message: t('cert.notify.removeSuccess'), duration: 5000 })
      await fetchStatus()
    } else {
      push.error({ message: res.data.msg || t('cert.notify.failed'), duration: 0 })
    }
  } catch (e: any) {
    push.error({ message: e?.response?.data?.msg || String(e), duration: 0 })
  } finally {
    issuing.value = ''
  }
}

onMounted(() => {
  fetchStatus()
})
</script>
