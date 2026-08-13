<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import type { CommandDefinition } from '../../types/commands'
import { commandSuggestions, commandTemplate, retargetDeviceCommand } from '../../utils/commandInput'
import {
  ArrowSwap24Regular,
  ArrowSync24Regular,
  CellularData124Regular,
  Code24Regular,
  List24Regular,
  Mail24Regular,
  Phone24Regular,
  QuestionCircle24Regular,
  Send24Regular,
  Sim24Regular,
  Status24Regular,
  Wallet24Regular
} from '@vicons/fluent'

const props = defineProps<{
  definitions: CommandDefinition[]
  busy: boolean
  selectedDevice: string
  deviceIds: string[]
}>()

const emit = defineEmits<{
  submit: [input: string]
  dangerous: [command: CommandDefinition]
}>()

const input = ref('')
const suggestions = computed(() => commandSuggestions(input.value, props.definitions))
const quickIcons: Readonly<Record<string, Component>> = Object.freeze({
  balance: Wallet24Regular,
  esim: Sim24Regular,
  help: QuestionCircle24Regular,
  list: List24Regular,
  rotate: ArrowSync24Regular,
  send: Send24Regular,
  signal: CellularData124Regular,
  sms: Mail24Regular,
  status: Status24Regular,
  switch: ArrowSwap24Regular,
  vocall: Phone24Regular
})

watch(() => props.selectedDevice, (device, previousDevice) => {
  input.value = retargetDeviceCommand(input.value, props.definitions, {
    selectedDevice: device,
    knownDeviceIDs: props.deviceIds,
    previousDevice
  })
})

function quickIcon(name: string): Component {
  return quickIcons[name] || Code24Regular
}

function choose(definition: CommandDefinition) {
  if (definition.dangerous) {
    emit('dangerous', definition)
    return
  }
  input.value = commandTemplate(definition, props.selectedDevice)
}

function submit() {
  const value = retargetDeviceCommand(input.value, props.definitions, {
    selectedDevice: props.selectedDevice,
    knownDeviceIDs: props.deviceIds
  }).trim()
  if (!value || props.busy) return
  emit('submit', value)
  input.value = ''
}
</script>

<template>
  <section class="composer" aria-label="命令输入">
    <div class="quick-list" aria-label="后端可用快捷命令">
      <button
        v-for="definition in definitions"
        :key="definition.name"
        type="button"
        :class="{ dangerous: definition.dangerous }"
        :aria-label="`${definition.summary}，命令 /${definition.name}`"
        @click="choose(definition)"
      >
        <el-icon aria-hidden="true"><component :is="quickIcon(definition.name)" /></el-icon>
        <span>/{{ definition.name }}</span>
      </button>
    </div>

    <Transition name="suggestions-pop">
      <div v-if="suggestions.length" class="suggestions" role="listbox" aria-label="命令建议">
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
    </Transition>

    <div class="input-row">
      <el-input
        v-model="input"
        maxlength="4096"
        placeholder="输入命令或点击上方快捷命令"
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
.composer {
  position: relative;
  padding: 10px 14px calc(14px + env(safe-area-inset-bottom));
  border-top: 1px solid var(--ui-border);
  background: var(--ui-surface);
}
.quick-list { display: flex; gap: 6px; overflow-x: auto; padding-bottom: 9px; scrollbar-width: thin; }
.quick-list button {
  min-height: 34px;
  padding: 0 10px;
  border: 1px solid var(--ui-border);
  border-radius: 4px;
  background: transparent;
  color: var(--ui-text-muted);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font: var(--ui-font-caption) "v-mono", ui-monospace, monospace;
  white-space: nowrap;
  cursor: pointer;
}
.quick-list button:hover, .quick-list button:focus-visible {
  border-color: var(--ui-primary);
  color: var(--ui-primary);
  outline: none;
}
.quick-list button:focus-visible { box-shadow: 0 0 0 2px color-mix(in srgb, var(--ui-primary) 24%, transparent); }
.quick-list button.dangerous { color: var(--ui-warning); }
.quick-list .el-icon { font-size: 14px; }
.input-row { display: grid; grid-template-columns: minmax(0, 1fr) 44px; gap: 8px; }
.send-button { width: 44px; height: 40px; padding: 0; border-radius: 5px; }
.input-row :deep(.el-input__wrapper) { min-height: 40px; border-radius: 5px; }
.suggestions {
  position: absolute;
  z-index: 12;
  left: 14px;
  right: 66px;
  bottom: 62px;
  max-height: 240px;
  overflow: auto;
  border: 1px solid var(--ui-border);
  border-radius: 6px;
  background: var(--ui-surface-strong);
  box-shadow: var(--ui-shadow-md);
}
.suggestions button {
  width: 100%;
  min-height: 48px;
  padding: 8px 12px;
  border: 0;
  border-bottom: 1px solid var(--ui-border);
  background: transparent;
  color: inherit;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  text-align: left;
}
.suggestions button:last-child { border-bottom: 0; }
.suggestions button:hover, .suggestions button:focus-visible { background: color-mix(in srgb, var(--ui-primary) 8%, transparent); outline: none; }
.suggestions strong { font: var(--ui-font-body-sm) "v-mono", monospace; }
.suggestions span { color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.suggestions-pop-enter-active { transition: opacity 160ms ease-out, transform 160ms ease-out; }
.suggestions-pop-leave-active { transition: opacity 100ms ease-in, transform 100ms ease-in; }
.suggestions-pop-enter-from, .suggestions-pop-leave-to { opacity: 0; transform: translateY(4px); }
@media (max-width: 1023px) { .composer { position: sticky; bottom: 0; z-index: 4; } }
@media (max-width: 820px) { .composer { bottom: calc(72px + env(safe-area-inset-bottom)); } }
@media (max-width: 640px) {
  .composer { padding-inline: 10px; }
  .quick-list button { min-height: 44px; }
  .quick-list button span { display: none; }
  .quick-list button { min-width: 44px; justify-content: center; padding: 0; }
  .input-row :deep(.el-input__wrapper), .send-button { min-height: 44px; height: 44px; }
}
@media (prefers-reduced-motion: reduce) {
  .suggestions-pop-enter-active, .suggestions-pop-leave-active { transition-property: opacity; }
  .suggestions-pop-enter-from, .suggestions-pop-leave-to { transform: none; }
}
</style>
