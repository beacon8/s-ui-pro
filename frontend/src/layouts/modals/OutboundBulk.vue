<template>
  <v-dialog transition="dialog-bottom-transition" width="800" :model-value="visible">
    <v-card class="rounded-lg">
      <v-card-title>
        {{ $t('actions.addbulk') }} {{ $t('objects.outbound') }}
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text style="padding: 0 16px; overflow-y: scroll;">
        <v-row v-if="outbounds.length==0">
          <v-col cols="12">
            <v-tabs v-model="mode" density="compact" color="primary" align-tabs="center">
              <v-tab value="url">{{ $t('client.sub') }}</v-tab>
              <v-tab value="text">{{ $t('out.pasteNodes') }}</v-tab>
            </v-tabs>
          </v-col>
          <v-col cols="12">
            <v-window v-model="mode">
              <v-window-item value="url">
                <v-text-field v-model="link"
                  dir="ltr"
                  :label="$t('client.sub')"
                  placeholder="http[s]://<domain>[:]<port>/<path>"
                  hide-details />
              </v-window-item>
              <v-window-item value="text">
                <v-textarea v-model="content"
                  dir="ltr"
                  rows="10"
                  :label="$t('out.pasteNodes')"
                  :placeholder="textPlaceholder"
                  hide-details />
              </v-window-item>
            </v-window>
          </v-col>
          <v-col cols="12">
            <v-checkbox v-model="addUrlTest" :label="$t('out.addUrlTest')" />
          </v-col>
          <v-col cols="12" align="center">
            <v-btn hide-details variant="tonal" :loading="loading" @click="submitConvert">{{ $t('submit') }}</v-btn>
          </v-col>
        </v-row>
        <v-data-table
          v-if="outbounds.length>0"
          :items="outbounds"
          :loading="loading"
          :items-per-page="0"
          hide-default-footer
          density="compact"
          :headers="[
            { value: 'check' },
            { title: $t('type'), value: 'type' },
            { title: $t('objects.tag'), value: 'tag' },
            { title: $t('out.addr'), value: 'server' },
            { title: $t('objects.tls'), value: 'tls' }
          ]"
        >
          <template v-slot:[`item.check`]="{ index }">
            <v-icon color="success" icon="mdi-check" v-if="outChecks[index]==1" />
            <v-icon color="error" icon="mdi-close" v-else-if="outChecks[index]==2" />
            <v-progress-circular v-else-if="outChecks[index]==3" indeterminate />
            <v-icon v-else icon="mdi-help"></v-icon>
          </template>
          <template v-slot:[`item.type`]="{ item }">
            {{ item.type }}
          </template>
          <template v-slot:[`item.tag`]="{ item }">
            {{ item.tag }}
          </template>
          <template v-slot:[`item.tls`]="{ item }">
            {{ Object.hasOwn(item,'tls') ? $t(item.tls?.enabled ? 'enable' : 'disable') : '-' }}
          </template>
          <template v-slot:[`item.server`]="{ item }">
            {{ item.server }}{{ item.server_port ? ':' + item.server_port : '' }}
          </template>
        </v-data-table>
      </v-card-text>
      <v-card-actions>
        <v-spacer></v-spacer>
        <v-btn color="primary" variant="outlined" @click="closeModal">{{ $t('actions.close') }}</v-btn>
        <v-btn color="primary" variant="tonal" :loading="loading" :disabled="outbounds.length==0" @click="saveChanges">{{ $t('actions.save') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import HttpUtils from '@/plugins/httputil'
import RandomUtil from '@/plugins/randomUtil';
import Data from '@/store/modules/data'
import { createOutbound, Outbound } from '@/types/outbounds'

export default {
  props: ['visible', 'outboundTags'],
  emits: ['close'],
  data() {
    return {
      loading: false,
      mode: 'url',
      link: "",
      content: "",
      outbounds: <Outbound[]>[],
      outChecks: <number[]>[],
      addUrlTest: false,
    }
  },
  methods: {
    resetData() {
      this.outbounds = []
      this.outChecks = []
      this.link = ""
      this.content = ""
      this.mode = 'url'
      this.addUrlTest = false
      this.loading = false
    },
    closeModal() {
      this.resetData()
      this.$emit('close')
    },
    async submitConvert() {
      this.loading = true
      this.outbounds = []
      const msg = this.mode === 'url'
        ? await HttpUtils.post('api/subConvert',     { link: this.link })
        : await HttpUtils.post('api/subConvertText', { content: this.content })
      if (msg.success && msg.obj?.length > 0) {
        this.fillOutbounds(msg.obj)
      }
      this.loading = false
    },
    fillOutbounds(list: any[]) {
      list.forEach((o: any, index: number) => {
        if (this.newOutboundTags.includes(o.tag)) o.tag = o.tag + "-" + (index + 1)
        this.outbounds.push(createOutbound(o.type, o))
        this.outChecks.push(0)
      })
      if (this.addUrlTest) {
        const urlTestTag = "urltest-" + RandomUtil.randomSeq(3)
        this.outbounds.push(createOutbound("urltest", {
          tag: urlTestTag,
          outbounds: this.outbounds.map((o: Outbound) => o.tag),
          interrupt_exist_connections: false,
          interval: "30s"
        }))
      }
    },
    async saveChanges() {
      if (!this.$props.visible) return

      // check duplicate tags first
      let hasDuplicate = false
      this.outbounds.forEach((o:Outbound, index:number) => {
        const isDuplicatedTag = Data().checkTag("outbound", 0, o.tag)
        this.outChecks[index] = isDuplicatedTag ? 2 : 0
        if (isDuplicatedTag) hasDuplicate = true
      })
      if (hasDuplicate) return

      // submit all at once
      this.loading = true
      const validOutbounds = this.outbounds.filter((_:Outbound, i:number) => this.outChecks[i] !== 2)
      const success = await Data().save("outbounds", "newbulk", validOutbounds)
      if (success) {
        this.outbounds.forEach((_:Outbound, i:number) => { this.outChecks[i] = 1 })
      } else {
        this.outbounds.forEach((_:Outbound, i:number) => { this.outChecks[i] = 2 })
      }
      this.loading = false
    }
  },
  computed: {
    newOutboundTags(): string[] {
      return this.outbounds.map((o:Outbound) => o.tag)
    },
    textPlaceholder(): string {
      return 'vmess://...\nvless://...\n1.2.3.4:1080#proxy01\n1.2.3.4:1080:user:pass#proxy02\nhttp://1.2.3.4:8080#proxy03'
    }
  },
  watch: {
    visible(v) {
      if (v) {
        this.resetData()
      }
    },
  },
}
</script>
