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
  emits: ['close'],
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
