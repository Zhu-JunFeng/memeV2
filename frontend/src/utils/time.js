export const BEIJING_TIME_ZONE = 'Asia/Shanghai'

const beijingFormatter = new Intl.DateTimeFormat('zh-CN', {
  timeZone: BEIJING_TIME_ZONE,
  hour12: false,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hourCycle: 'h23',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit'
})

function partsOf(value) {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return Object.fromEntries(beijingFormatter.formatToParts(date).map((part) => [part.type, part.value]))
}

export function formatBeijingDateTime(value) {
  const parts = partsOf(value)
  if (!parts) return ''
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}

export function formatBeijingRFC3339(value) {
  const parts = partsOf(value)
  if (!parts) return ''
  return `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}:${parts.second}+08:00`
}

export function toUnixTimestamp(value) {
  const timestamp = Math.floor(new Date(value).getTime() / 1000)
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : null
}

export function formatChartTick(value) {
  const seconds = typeof value === 'number' ? value : Date.parse(value) / 1000
  if (!Number.isFinite(seconds)) return ''
  const text = formatBeijingDateTime(seconds * 1000)
  return text ? text.slice(5, 16) : ''
}
