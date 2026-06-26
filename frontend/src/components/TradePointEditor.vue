<template>
  <section class="panel">
    <div class="panel-title">买卖点</div>
    <div class="trade-editor-row">
      <el-select v-model="draft.side" class="side-select">
        <el-option label="买入" value="buy" />
        <el-option label="卖出" value="sell" />
      </el-select>
      <el-date-picker v-model="draft.time" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" placeholder="北京时间" />
      <el-input v-model="draft.note" placeholder="备注，可选" />
      <el-button type="primary" @click="addPoint">添加</el-button>
    </div>
    <el-input v-model="bulkText" type="textarea" :rows="4" placeholder="批量输入：每行 side,time,note，例如 buy,2026-06-22T08:00:00+08:00,首次买入" />
    <div class="editor-actions">
      <el-button @click="importBulk">导入文本</el-button>
      <el-button @click="$emit('update:modelValue', [])">清空</el-button>
    </div>
    <el-table :data="modelValue" size="small" class="trade-table">
      <el-table-column prop="side" label="方向" width="80" />
      <el-table-column prop="time" label="时间" min-width="190" />
      <el-table-column prop="note" label="备注" />
      <el-table-column label="操作" width="80">
        <template #default="{ $index }">
          <el-button link type="danger" @click="removePoint($index)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </section>
</template>

<script setup>
import { reactive, ref } from 'vue'

const props = defineProps({ modelValue: { type: Array, default: () => [] } })
const emit = defineEmits(['update:modelValue'])

const draft = reactive({ side: 'buy', time: '', note: '' })
const bulkText = ref('')

function addPoint() {
  if (!draft.time) return
  emit('update:modelValue', [...props.modelValue, { side: draft.side, time: draft.time, note: draft.note }])
  draft.note = ''
}

function removePoint(index) {
  emit('update:modelValue', props.modelValue.filter((_, itemIndex) => itemIndex !== index))
}

function importBulk() {
  const imported = bulkText.value.split('\n').map((line) => {
    const [side, time, ...noteParts] = line.split(',').map((item) => item.trim())
    return { side, time, note: noteParts.join(',') }
  }).filter((item) => ['buy', 'sell'].includes(item.side) && item.time)
  emit('update:modelValue', [...props.modelValue, ...imported])
}
</script>
