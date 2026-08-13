<script lang="ts" setup>
import {computed, type Component} from 'vue'
import {
  Activity,
  ArrowRight,
  ArrowUp,
  Check,
  ChevronDown,
  CircleAlert,
  Clock,
  Command,
  FileText,
  Folder,
  FolderCog,
  KeyRound,
  Lock,
  Package,
  Palette,
  PanelRight,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Settings,
  Trash2,
  Waypoints,
  X,
  Zap,
} from 'lucide-vue-next'

const map: Record<string, Component> = {
  terminal: Command,
  tunnel: Waypoints,
  activity: Activity,
  settings: Settings,
  gear: Settings,
  folder: Folder,
  'folder-cog': FolderCog,
  file: FileText,
  close: X,
  x: X,
  plus: Plus,
  key: KeyRound,
  refresh: RefreshCw,
  up: ArrowUp,
  'arrow-right': ArrowRight,
  'chevron-down': ChevronDown,
  lock: Lock,
  search: Search,
  command: Command,
  'panel-right': PanelRight,
  pencil: Pencil,
  trash: Trash2,
  palette: Palette,
  package: Package,
  clock: Clock,
  zap: Zap,
  check: Check,
}

const props = defineProps<{name: string; size?: number; extraClass?: string}>()
const comp = computed(() => map[props.name] ?? CircleAlert)
</script>

<template>
  <component
    :is="comp"
    :size="size ?? 16"
    :stroke-width="1.6"
    class="block shrink-0"
    :class="extraClass"
    aria-hidden="true"
  />
</template>
