<script setup lang="ts">
import {t as $t} from '../../i18n'
import {nextTick, onMounted} from 'vue'
import Icon from '../Icon.vue'
import {useMenuPlacement} from '../../composables/useMenuPlacement'

const props = defineProps<{path: string; position: {x: number; y: number}}>()
const emit = defineEmits<{
  createChild: []
  newServer: []
  rename: []
  remove: []
  close: []
}>()

const {menuEl, menuStyle, place} = useMenuPlacement()

function items(): HTMLElement[] {
  return Array.from(menuEl.value?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])') ?? [])
}

function focusAt(index: number) {
  const list = items()
  if (!list.length) return
  list[(index + list.length) % list.length].focus()
}

function onKeydown(event: KeyboardEvent) {
  const list = items()
  const current = list.indexOf(document.activeElement as HTMLElement)
  if (event.key === 'Escape') {
    event.preventDefault()
    emit('close')
  } else if (event.key === 'ArrowDown') {
    event.preventDefault()
    focusAt(current + 1)
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    focusAt(current - 1)
  } else if (event.key === 'Home') {
    event.preventDefault()
    focusAt(0)
  } else if (event.key === 'End') {
    event.preventDefault()
    focusAt(list.length - 1)
  }
}

onMounted(async () => {
  await nextTick()
  focusAt(0)
  void place(props.position.x, props.position.y)
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="menuEl"
      class="menu-pop neo fixed"
      role="menu"
      :aria-label="$t('分组操作')"
      data-ui="context-menu"
      data-testid="group-context-menu"
      :style="menuStyle"
      @contextmenu.prevent
      @click.stop
      @keydown="onKeydown"
    >
      <div class="px-2.5 pt-1.5 pb-1 text-[11px] text-mist truncate" role="presentation">{{ path }}</div>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('createChild')">
        <Icon name="folder-plus" :size="13" />{{ $t('新建子分组') }}
      </button>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('newServer')">
        <Icon name="server" :size="13" />{{ $t('在此新建服务器') }}
      </button>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('rename')">
        <Icon name="pencil" :size="13" />{{ $t('重命名') }}
      </button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" class="danger !flex items-center gap-1.5" type="button" @click="emit('remove')">
        <Icon name="trash" :size="13" />{{ $t('删除分组') }}
      </button>
    </div>
  </Teleport>
</template>
