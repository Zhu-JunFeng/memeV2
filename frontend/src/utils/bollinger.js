export const DEFAULT_BOLLINGER_OPTIONS = Object.freeze({
  length: 20,
  std: 2,
  ddof: 0,
});

export function calculatePandasTaBbands(items, options = {}) {
  const length = normalizeInteger(options.length, DEFAULT_BOLLINGER_OPTIONS.length);
  const std = normalizeNumber(options.std, DEFAULT_BOLLINGER_OPTIONS.std);
  const ddof = normalizeDdof(options.ddof, length);
  const valueAccessor =
    typeof options.valueAccessor === "function"
      ? options.valueAccessor
      : (item) => item?.close;

  if (length <= 0 || std <= 0 || !Array.isArray(items) || items.length < length) {
    return [];
  }

  const values = items.map((item) => Number(valueAccessor(item)));
  const result = [];
  for (let index = length - 1; index < items.length; index += 1) {
    const start = index - length + 1;
    let sum = 0;
    let valid = true;
    for (let cursor = start; cursor <= index; cursor += 1) {
      if (!Number.isFinite(values[cursor])) {
        valid = false;
        break;
      }
      sum += values[cursor];
    }
    if (!valid) continue;

    const middle = sum / length;
    let varianceSum = 0;
    for (let cursor = start; cursor <= index; cursor += 1) {
      varianceSum += (values[cursor] - middle) ** 2;
    }
    const denominator = length - ddof;
    if (denominator <= 0) continue;

    const deviation = Math.sqrt(varianceSum / denominator) * std;
    const lower = middle - deviation;
    const upper = middle + deviation;
    result.push({
      time: items[index]?.time,
      index,
      lower,
      middle,
      upper,
      bandwidth: middle === 0 ? null : ((upper - lower) / middle) * 100,
      percent: upper === lower ? null : (values[index] - lower) / (upper - lower),
      value: values[index],
    });
  }
  return result;
}

function normalizeInteger(value, fallback) {
  const number = Number(value);
  return Number.isInteger(number) && number > 0 ? number : fallback;
}

function normalizeNumber(value, fallback) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

function normalizeDdof(value, length) {
  const number = Number(value);
  return Number.isInteger(number) && number >= 0 && number < length ? number : 1;
}
