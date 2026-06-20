/**
 * plugins/vuetify.ts
 *
 * Ant Design 风格主题适配
 */

// Styles
import '@mdi/font/css/materialdesignicons.css'
import 'vuetify/styles/main.css'

import { fa, en, vi, zhHans, zhHant, ru } from 'vuetify/locale'

// Composables
import { createVuetify } from 'vuetify'

// https://vuetifyjs.com/en/introduction/why-vuetify/#feature-guides
export default createVuetify({
  defaults: {
    VRow: { density: 'compact' },
    VTextField: {
      variant: 'outlined',
      density: 'comfortable',
      hideDetails: 'auto',
    },
    VSelect: {
      variant: 'outlined',
      density: 'comfortable',
      hideDetails: 'auto',
    },
    VCombobox: {
      variant: 'outlined',
      density: 'comfortable',
      hideDetails: 'auto',
    },
    VTextarea: {
      variant: 'outlined',
      density: 'comfortable',
      hideDetails: 'auto',
    },
    VBtn: {
      density: 'comfortable',
    },
    VCard: {
      rounded: 'lg',
      border: 'thin',
    },
    VChip: {
      density: 'comfortable',
    },
  },
  theme: {
    defaultTheme: localStorage.getItem('theme') ?? 'system',
    themes: {
      light: {
        colors: {
          // Antd 5 默认蓝
          primary: '#1677ff',
          secondary: '#1677ff',
          error: '#ff4d4f',
          warning: '#faad14',
          success: '#52c41a',
          info: '#1677ff',
          background: '#f5f5f5',
          surface: '#ffffff',
        },
      },
      dark: {
        colors: {
          primary: '#1668dc',
          secondary: '#1668dc',
          error: '#ff7875',
          warning: '#d48806',
          success: '#49aa19',
          info: '#1668dc',
          background: '#141414',
          surface: '#1f1f1f',
        },
      },
    },
  },
  locale: {
    locale: localStorage.getItem("locale") ?? 'zhHans',
    fallback: 'zhHans',
    messages: { en, fa, vi, zhHans, zhHant, ru },
  },
})
