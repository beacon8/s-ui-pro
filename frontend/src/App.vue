<template>
  <v-overlay
    :model-value="loading"
    persistent
    content-class="text-center"
    class="align-center justify-center"
  >
    <v-progress-circular
      indeterminate
      size="64"
    ></v-progress-circular>
    <br />
    {{ $t('loading') }}
  </v-overlay>
  <Message />
  <router-view />
</template>

<script lang="ts" setup>
import Message from '@/components/message.vue'
import { inject, ref, Ref } from 'vue'

const loading:Ref = inject('loading')?? ref(false)

// Change page title
document.title = "S-UI " + document.location.hostname
</script>

<style>
.v-overlay .v-list-item,
.v-field__input {
  direction: ltr;
}

/* ===== Ant Design 风格全局覆盖 ===== */

/* 卡片：Antd 风格边框 + 柔和阴影 */
.v-card {
  border-color: #f0f0f0 !important;
  box-shadow: 0 1px 2px -2px rgba(0,0,0,0.08), 0 3px 6px 0 rgba(0,0,0,0.06) !important;
  overflow: hidden;
}
.v-card--variant-elevated {
  box-shadow: 0 1px 2px -2px rgba(0,0,0,0.08), 0 3px 6px 0 rgba(0,0,0,0.06) !important;
}

/* 按钮：Antd 默认尺寸，更紧凑 */
.v-btn {
  text-transform: none;
  font-weight: 400;
}
.v-btn--density-compact {
  height: 32px;
}
.v-btn--variant-tonal {
  background: #e6f4ff !important;
  color: #1677ff !important;
}

/* 表格：Antd 风格行高 + 表头背景 */
.v-data-table {
  background: #fff;
}
.v-data-table__th {
  background: #fafafa !important;
  font-weight: 500;
  border-bottom: 1px solid #f0f0f0 !important;
}
.v-data-table__td {
  border-bottom: 1px solid #f0f0f0 !important;
}
.v-data-table__tr:hover .v-data-table__td {
  background: #fafafa !important;
}

/* 芯片/标签：Antd Tag 风格 */
.v-chip {
  border-radius: 4px;
  font-size: 13px;
}
.v-chip--variant-flat {
  border: 1px solid transparent;
}

/* 输入框：outlined 加淡灰背景，在白卡片上有层次感 */
.v-field--variant-outlined {
  background-color: #fafafa;
}
.v-field--variant-outlined:hover:not(.v-field--focused) {
  background-color: #f5f5f5;
}
.v-field--variant-outlined.v-field--focused {
  background-color: #fff;
}
/* 边框加深 */
.v-field--variant-outlined .v-field__outline__start,
.v-field--variant-outlined .v-field__outline__end,
.v-field--variant-outlined .v-field__outline__notch::before,
.v-field--variant-outlined .v-field__outline__notch::after {
  border-color: #bfbfbf;
}
.v-field--variant-outlined.v-field--focused .v-field__outline__start,
.v-field--variant-outlined.v-field--focused .v-field__outline__end,
.v-field--variant-outlined.v-field--focused .v-field__outline__notch::before,
.v-field--variant-outlined.v-field--focused .v-field__outline__notch::after {
  border-color: #1677ff;
}
.v-field--variant-outlined:hover:not(.v-field--focused) .v-field__outline__start,
.v-field--variant-outlined:hover:not(.v-field--focused) .v-field__outline__end,
.v-field--variant-outlined:hover:not(.v-field--focused) .v-field__outline__notch::before,
.v-field--variant-outlined:hover:not(.v-field--focused) .v-field__outline__notch::after {
  border-color: #8c8c8c;
}

/* 表单字段间距：防止连续输入框挤在一起 */
.v-input--density-compact {
  margin-bottom: 8px;
}

/* 分隔线 */
.v-divider {
  border-color: #f0f0f0;
}

/* 导航抽屉：Antd Sider 风格 */
.v-navigation-drawer {
  border-right: 1px solid #f0f0f0;
}
.v-list-item--variant-text .v-list-item__overlay {
  background: transparent;
}
.v-list-item--nav .v-list-item-title {
  font-size: 14px;
}

/* 暗色主题适配 */
.v-theme--dark .v-card {
  border-color: #303030 !important;
}
.v-theme--dark .v-data-table__th {
  background: #1f1f1f !important;
  border-color: #303030 !important;
}
.v-theme--dark .v-data-table__td {
  border-color: #303030 !important;
}
.v-theme--dark .v-divider {
  border-color: #303030;
}
.v-theme--dark .v-field--variant-outlined {
  background-color: rgba(255,255,255,0.04);
}
.v-theme--dark .v-field--variant-outlined:hover:not(.v-field--focused) {
  background-color: rgba(255,255,255,0.06);
}
.v-theme--dark .v-field--variant-outlined.v-field--focused {
  background-color: rgba(255,255,255,0.02);
}
.v-theme--dark .v-field--variant-outlined .v-field__outline__start,
.v-theme--dark .v-field--variant-outlined .v-field__outline__end,
.v-theme--dark .v-field--variant-outlined .v-field__outline__notch::before,
.v-theme--dark .v-field--variant-outlined .v-field__outline__notch::after {
  border-color: #595959;
}
.v-theme--dark .v-field--variant-outlined:hover:not(.v-field--focused) .v-field__outline__start,
.v-theme--dark .v-field--variant-outlined:hover:not(.v-field--focused) .v-field__outline__end,
.v-theme--dark .v-field--variant-outlined:hover:not(.v-field--focused) .v-field__outline__notch::before,
.v-theme--dark .v-field--variant-outlined:hover:not(.v-field--focused) .v-field__outline__notch::after {
  border-color: #8c8c8c;
}
.v-theme--dark .v-navigation-drawer {
  border-right-color: #303030;
}
</style>