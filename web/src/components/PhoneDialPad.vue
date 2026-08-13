<script setup lang="ts">
defineProps<{ disabled?: boolean }>()

defineEmits<{ digit: [digit: string] }>()

const keys = [
  { digit: '1', letters: '' }, { digit: '2', letters: 'ABC' }, { digit: '3', letters: 'DEF' },
  { digit: '4', letters: 'GHI' }, { digit: '5', letters: 'JKL' }, { digit: '6', letters: 'MNO' },
  { digit: '7', letters: 'PQRS' }, { digit: '8', letters: 'TUV' }, { digit: '9', letters: 'WXYZ' },
  { digit: '*', letters: '' }, { digit: '0', letters: '+' }, { digit: '#', letters: '' }
]
</script>

<template>
  <div class="dial-pad" aria-label="电话拨号盘">
    <button
      v-for="key in keys"
      :key="key.digit"
      type="button"
      class="dial-key"
      :disabled="disabled"
      :aria-label="`按键 ${key.digit}${key.letters ? `，${key.letters}` : ''}`"
      @click="$emit('digit', key.digit)"
    >
      <span>{{ key.digit }}</span>
      <small>{{ key.letters || '&nbsp;' }}</small>
    </button>
  </div>
</template>

<style scoped>
.dial-pad {
  width: 276px;
  max-width: 100%;
  margin: 0 auto;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}

.dial-key {
  height: 62px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  border: 1px solid var(--ui-border);
  border-radius: 6px;
  background: var(--ui-surface-muted);
  color: var(--ui-text);
  cursor: pointer;
  transition: border-color 160ms ease, background-color 160ms ease, transform 120ms ease;
}

.dial-key:hover:not(:disabled) {
  border-color: var(--ui-primary);
  background: color-mix(in srgb, var(--ui-primary) 9%, var(--ui-surface));
}

.dial-key:active:not(:disabled) { transform: scale(.96); }
.dial-key:disabled { cursor: not-allowed; opacity: .45; }
.dial-key span { font-family: "v-mono", monospace; font-size: 22px; line-height: 1; }
.dial-key small { min-height: 12px; margin-top: 4px; color: var(--ui-text-muted); font-size: 9px; letter-spacing: .12em; }

@media (prefers-reduced-motion: reduce) {
  .dial-key { transition: none; }
}
</style>
