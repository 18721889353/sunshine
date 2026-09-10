<template>
  <el-form-item :label="label" :prop="prop">
    <el-input
      v-if="type === 'text'"
      :model-value="modelValue"
      :placeholder="placeholder"
      clearable
      @update:model-value="$emit('update:modelValue', $event)"
    />
    <el-select
      v-else-if="type === 'select'"
      :model-value="modelValue"
      :placeholder="placeholder"
      clearable
      @update:model-value="$emit('update:modelValue', $event)"
    >
      <el-option v-for="opt in options" :key="opt.value" :label="opt.label" :value="opt.value" />
    </el-select>
    <el-switch
      v-else-if="type === 'switch'"
      :model-value="modelValue"
      @update:model-value="$emit('update:modelValue', $event)"
    />
    <el-radio-group
      v-else-if="type === 'radio'"
      :model-value="modelValue"
      @update:model-value="$emit('update:modelValue', $event)"
    >
      <el-radio v-for="opt in options" :key="opt.value" :value="opt.value">{{ opt.label }}</el-radio>
    </el-radio-group>
  </el-form-item>
</template>

<script setup>
defineProps({
  type: { type: String, default: 'text' },
  label: { type: String, default: '' },
  prop: { type: String, default: '' },
  modelValue: { type: [String, Number, Boolean], default: '' },
  placeholder: { type: String, default: '' },
  options: { type: Array, default: () => [] },
})
defineEmits(['update:modelValue'])
</script>
