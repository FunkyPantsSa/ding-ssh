<script setup lang="ts">
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

import {onMounted} from 'vue'
import {useMenuKeyboard} from '../../composables/useMenuKeyboard'
import Icon from '../Icon.vue'
import type {ServerNode} from '../../types'
import {useMenuPlacement} from '../../composables/useMenuPlacement'

const props = defineProps<{node: ServerNode; position: {x: number; y: number}; copied: boolean; cloning: boolean}>()
const emit = defineEmits<{edit: []; copy: []; duplicate: []; test: []; remove: []; close: []}>()
// 打开后按真实高度定位，贴近窗口底部时上移，避免菜单项被裁掉
const {menuEl, menuStyle, place} = useMenuPlacement()

const {onKeydown} = useMenuKeyboard(menuEl, () => emit('close'))

onMounted(() => { void place(props.position.x, props.position.y) })
</script>
<template>
  <Teleport to="body">
    <div
      ref="menuEl"
      class="menu-pop neo fixed"
      role="menu"
      :aria-label="$t('服务器操作')"
      data-ui="context-menu"
      data-testid="server-context-menu"
      :style="menuStyle"
      @contextmenu.prevent
      @click.stop
      @keydown="onKeydown"
    >
      <div class="px-2.5 pt-1.5 pb-1 text-[11px] text-mist truncate" role="presentation">{{ node.name }}</div>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('edit')"><Icon name="pencil" :size="13" />{{ $t('编辑') }}</button>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('copy')"><Icon :name="copied ? 'check' : 'clipboard-copy'" :size="13" />{{ copied ? t('已复制IP') : t('复制IP') }}</button>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" :disabled="cloning" @click="emit('duplicate')"><Icon name="copy" :size="13" />{{ $t('复制配置（生成 .new）') }}</button>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('test')"><Icon name="activity" :size="13" />{{ $t('测试在线状态') }}</button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" class="danger !flex items-center gap-1.5" type="button" @click="emit('remove')"><Icon name="trash" :size="13" />{{ $t('删除') }}</button>
    </div>
  </Teleport>
</template>
