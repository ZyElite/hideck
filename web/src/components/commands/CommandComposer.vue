<script setup lang="ts">
import { computed, ref } from 'vue'
import type { CommandDefinition } from '../../types/commands'
import { commandSuggestions, commandTemplate } from '../../utils/commandInput'
import { Send24Regular } from '@vicons/fluent'

const props = defineProps<{
  definitions: CommandDefinition[]
  busy: boolean
  selectedDevice: string
}>()

const emit = defineEmits<{
  submit: [input: string]
  dangerous: [command: CommandDefinition]
}>()

const input = ref('')
const suggestions = computed(() => commandSuggestions(input.value, props.definitions))

function choose(definition: CommandDefinition) {
  if (definition.dangerous) {
    emit('dangerous', definition)
    return
  }
  input.value = commandTemplate(definition, props.selectedDevice)
}

function submit() {
  const value = input.value.trim()
  if (!value || props.busy) return
  emit('submit', value)
  input.value = ''
}
</script>

<template>
  <section class="composer" aria-label="命令输入">
    <div class="quick-list" aria-label="快捷命令">
      <button
        v-for="definition in definitions"
        :key="definition.name"
        type="button"
        :class="{ dangerous: definition.dangerous }"
        @click="choose(definition)"
      >
        /{{ definition.name }}
      </button>
    </div>
    <div v-if="suggestions.length" class="suggestions" role="listbox">
      <button
        v-for="definition in suggestions"
        :key="definition.name"
        type="button"
        role="option"
        @click="choose(definition)"
      >
        <strong>{{ definition.usage }}</strong>
        <span>{{ definition.summary }}</span>
      </button>
    </div>
    <div class="input-row">
      <el-input
        v-model="input"
        size="large"
        maxlength="4096"
        placeholder="输入命令"
        aria-label="输入斜杠命令"
        @keydown.enter.exact.prevent="submit"
      />
      <el-tooltip content="执行命令" placement="top">
        <el-button class="send-button" type="primary" :loading="busy" :disabled="!input.trim()" @click="submit">
          <el-icon><Send24Regular /></el-icon>
        </el-button>
      </el-tooltip>
    </div>
  </section>
</template>

<style scoped>
.composer { position: relative; padding: 10px 12px calc(12px + env(safe-area-inset-bottom)); border-top: 1px solid var(--ui-border); background: var(--ui-surface-strong); }
.quick-list { display: flex; gap: 6px; overflow-x: auto; padding-bottom: 9px; scrollbar-width: thin; }
.quick-list button { min-height: 44px; padding: 0 10px; border: 1px solid var(--ui-border); border-radius: 6px; background: transparent; color: #475569; font: 12px "v-mono", monospace; white-space: nowrap; }
.quick-list button:hover, .quick-list button:focus-visible { border-color: #0d9488; color: #0f766e; outline: none; }
.quick-list button.dangerous { color: #b45309; }
.input-row { display: grid; grid-template-columns: minmax(0, 1fr) 44px; gap: 8px; }
.send-button { width: 44px; height: 44px; padding: 0; }
.input-row :deep(.el-input__wrapper) { min-height: 44px; }
.suggestions { position: absolute; z-index: 12; left: 12px; right: 64px; bottom: 58px; max-height: 240px; overflow: auto; border: 1px solid var(--ui-border); border-radius: 8px; background: var(--ui-surface-strong); box-shadow: var(--ui-shadow-md); }
.suggestions button { width: 100%; min-height: 48px; padding: 8px 12px; border: 0; border-bottom: 1px solid var(--ui-border); background: transparent; color: inherit; display: flex; flex-direction: column; align-items: flex-start; gap: 2px; text-align: left; }
.suggestions button:last-child { border-bottom: 0; }
.suggestions button:hover, .suggestions button:focus-visible { background: rgba(13, 148, 136, .08); outline: none; }
.suggestions strong { font: 12px "v-mono", monospace; }
.suggestions span { color: #64748b; font-size: 11px; }
:global(html.dark) .quick-list button { color: #cbd5e1; }
:global(html.dark) .quick-list button.dangerous { color: #fbbf24; }
@media (max-width: 1023px) { .composer { position: sticky; bottom: 0; z-index: 4; } }
</style>
