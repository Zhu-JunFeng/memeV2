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

export function formatHoldingDuration(openedAt, closedAt, now = Date.now()) {
  const start = new Date(openedAt).getTime()
  const end = closedAt ? new Date(closedAt).getTime() : Number(now)
  if (!Number.isFinite(start) || !Number.isFinite(end)) return '-'

  let seconds = Math.max(0, Math.floor((end - start) / 1000))
  const days = Math.floor(seconds / 86400)
  seconds %= 86400
  const hours = Math.floor(seconds / 3600)
  seconds %= 3600
  const minutes = Math.floor(seconds / 60)
  seconds %= 60

  if (days > 0) return `${days}d${hours}h${minutes}m`
  if (hours > 0) return `${hours}h${minutes}m${seconds}s`
  if (minutes > 0) return `${minutes}m${seconds}s`
  return `${seconds}s`
}

export function toUnixTimestamp(value) {
  const timestamp = Math.floor(new Date(value).getTime() / 1000)
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : null
}

function normalizeChartTimeValue(value) {
  if (typeof value === 'number') return value
  if (value && typeof value === 'object') {
    if (typeof value.timestamp === 'number') return value.timestamp
    if (
      typeof value.year === 'number' &&
      typeof value.month === 'number' &&
      typeof value.day === 'number'
    ) {
      return toUnixTimestamp(
        `${value.year}-${String(value.month).padStart(2, '0')}-${String(value.day).padStart(2, '0')}T00:00:00+08:00`
      )
    }
  }
  const timestamp = Date.parse(value) / 1000
  return Number.isFinite(timestamp) ? timestamp : null
}

export function formatChartTick(value) {
  const seconds = normalizeChartTimeValue(value)
  if (!Number.isFinite(seconds)) return ''
  const text = formatBeijingDateTime(seconds * 1000)
  return text ? text.slice(5, 16) : ''
}

export function formatChartCrosshairTime(value) {
  const seconds = normalizeChartTimeValue(value)
  if (!Number.isFinite(seconds)) return ''
  const text = formatBeijingDateTime(seconds * 1000)
  return text ? text.slice(5) : ''
}
