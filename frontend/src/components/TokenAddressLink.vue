<template>
  <div class="token-link" :class="{ compact }">
    <a
      v-if="normalized"
      class="token-link__anchor"
      :href="gmgnUrl"
      target="_blank"
      rel="noopener noreferrer"
      :title="normalized"
    >
      {{ displayText }}
    </a>
    <span v-else class="token-link__empty">-</span>
    <div v-if="normalized" class="token-link__actions">
      <el-button link type="primary" size="small" @click.stop="openGmgn"
        >GMGN</el-button
      >
      <el-button link size="small" @click.stop="copyAddress">复制</el-button>
    </div>
  </div>
</template>

<script setup>
import { computed } from "vue";
import { ElMessage } from "element-plus";

const props = defineProps({
  address: { type: String, default: "" },
  short: { type: Boolean, default: true },
  compact: { type: Boolean, default: false },
});

const normalized = computed(() => String(props.address || "").trim());
const gmgnUrl = computed(() =>
  normalized.value ? `https://gmgn.ai/sol/token/${normalized.value}` : "",
);
const displayText = computed(() => {
  if (!normalized.value) return "-";
  if (!props.short || normalized.value.length <= 18) return normalized.value;
  return `${normalized.value.slice(0, 8)}...${normalized.value.slice(-8)}`;
});

function openGmgn() {
  if (!gmgnUrl.value) return;
  window.open(gmgnUrl.value, "_blank", "noopener,noreferrer");
}

async function copyAddress() {
  if (!normalized.value) return;
  try {
    await copyText(normalized.value);
    ElMessage.success("CA 已复制");
  } catch (error) {
    ElMessage.error("复制失败");
  }
}

async function copyText(text) {
  if (
    window.isSecureContext &&
    navigator.clipboard &&
    typeof navigator.clipboard.writeText === "function"
  ) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.top = "-9999px";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  textarea.setSelectionRange(0, textarea.value.length);
  try {
    const copied = document.execCommand("copy");
    if (!copied) {
      throw new Error("execCommand copy failed");
    }
  } finally {
    document.body.removeChild(textarea);
  }
}
</script>

<style scoped>
.token-link {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.token-link.compact {
  gap: 6px;
}

.token-link__anchor {
  min-width: 0;
  color: #2563eb;
  font-weight: 600;
  text-decoration: none;
  word-break: break-all;
}

.token-link__anchor:hover {
  color: #1d4ed8;
  text-decoration: underline;
}

.token-link__actions {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

.token-link__actions :deep(.el-button) {
  margin-left: 0;
  padding: 0 2px;
}

.token-link__empty {
  color: #94a3b8;
}
</style>
