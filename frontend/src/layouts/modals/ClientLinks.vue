<template>
  <v-dialog transition="dialog-bottom-transition" width="600">
    <v-card class="rounded-lg" id="links-modal" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col>{{ $t('client.links') }}</v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto"><v-icon icon="mdi-close-box" @click="$emit('close')" /></v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-skeleton-loader
          class="mx-auto border"
          width="80%"
          type="text, divider, text, divider, text"
          v-if="loading"
        ></v-skeleton-loader>
      <v-card-text style="overflow-y: auto; padding: 0" :hidden="loading">
        <v-tabs
          v-model="tab"
          density="compact"
          fixed-tabs
          align-tabs="center"
        >
          <v-tab value="sub">{{ $t('setting.sub') }}</v-tab>
          <v-tab value="link">{{ $t('client.links') }}</v-tab>
        </v-tabs>
        <v-window v-model="tab" style="margin-top: 10px;">
          <v-window-item value="sub" class="px-4 pb-4">
            <v-text-field
              readonly
              variant="outlined"
              density="compact"
              :label="$t('setting.sub')"
              :model-value="clientSub"
              append-inner-icon="mdi-content-copy"
              @click:append-inner="copyToClipboard(clientSub)"
            ></v-text-field>
            <v-text-field
              readonly
              variant="outlined"
              density="compact"
              :label="$t('setting.jsonSub')"
              :model-value="clientSub + '?format=json'"
              append-inner-icon="mdi-content-copy"
              @click:append-inner="copyToClipboard(clientSub + '?format=json')"
            ></v-text-field>
            <v-text-field
              readonly
              variant="outlined"
              density="compact"
              :label="$t('setting.clashSub')"
              :model-value="clientSub + '?format=clash'"
              append-inner-icon="mdi-content-copy"
              @click:append-inner="copyToClipboard(clientSub + '?format=clash')"
            ></v-text-field>
            <v-text-field
              readonly
              variant="outlined"
              density="compact"
              label="SING-BOX"
              :model-value="singbox"
              append-inner-icon="mdi-content-copy"
              @click:append-inner="copyToClipboard(singbox)"
            ></v-text-field>
          </v-window-item>
          <v-window-item value="link" class="px-4 pb-4">
            <div v-for="l in displayLinks" :key="l.uri" class="mb-3">
              <v-chip size="small" class="mb-1">{{ client.name }}</v-chip>
              <v-text-field
                readonly
                variant="outlined"
                density="compact"
                :model-value="l.uri"
                append-inner-icon="mdi-content-copy"
                @click:append-inner="copyToClipboard(l.uri)"
              ></v-text-field>
            </div>
          </v-window-item>
        </v-window>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import Data from '@/store/modules/data'
import Clipboard from 'clipboard'
import { i18n } from '@/locales'
import { push } from 'notivue'

export default {
  props: ['id', 'visible'],
  data() {
    return {
      tab: "sub",
      client: <any>{},
      loading: false,
    }
  },
  methods: {
    async load() {
      this.loading = true
      const newData = await Data().loadClients(this.$props.id)
      this.client = newData
      this.loading = false
    },
    copyToClipboard(txt: string) {
      const hiddenButton = document.createElement('button')
      hiddenButton.className = 'clipboard-btn'
      document.body.appendChild(hiddenButton)

      const clipboard = new Clipboard('.clipboard-btn', {
        text: () => txt,
        container: document.getElementById('links-modal') ?? undefined
      });

      clipboard.on('success', () => {
        clipboard.destroy()
        push.success({
          message: i18n.global.t('success') + ": " + i18n.global.t('copyToClipboard'),
          duration: 5000,
        })
      })

      clipboard.on('error', () => {
        clipboard.destroy()
        push.error({
          message: i18n.global.t('failed') + ": " + i18n.global.t('copyToClipboard'),
          duration: 5000,
        })
      })

      hiddenButton.click()
      document.body.removeChild(hiddenButton)
    }
  },
  computed: {
    clientSub() {
      return Data().subURI + this.client.name
    },
    singbox() {
      const url = Data().subURI + this.client.name + "?format=json"
      return "sing-box://import-remote-profile?url=" + encodeURIComponent(url) + "#" + this.client.name
    },
    clientLinks() {
      return this.client.links ?? []
    },
    displayLinks() {
      const name = this.client.name ?? ''
      return this.clientLinks.map((l: any) => {
        const uri = l.uri ?? ''
        const idx = uri.lastIndexOf('#')
        return { ...l, uri: idx >= 0 ? uri.slice(0, idx + 1) + encodeURIComponent(name) : uri }
      })
    },
  },
  watch: {
    visible(v) {
      if (v) {
        this.tab = "sub"
        this.load()
      }
    },
  },
}
</script>
