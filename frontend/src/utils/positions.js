export function summarizeOpenPositions(items = []) {
  return items
    .filter((item) => item?.status === "open")
    .reduce(
      (total, item) => {
        const cost = finiteNumber(item.costAmount);
        const realizedPnl = finiteNumber(item.realizedPnl);
        const unrealizedPnl = finiteNumber(item.unrealizedPnl);
        const pnl = realizedPnl + unrealizedPnl;
        const storedMarketValue = finiteNumber(item.marketValue);

        total.cost += cost;
        total.marketValue += storedMarketValue > 0 ? storedMarketValue : Math.max(0, cost + pnl);
        total.totalPnl += pnl;
        return total;
      },
      { cost: 0, marketValue: 0, totalPnl: 0 },
    );
}

function finiteNumber(value) {
  const number = Number(value || 0);
  return Number.isFinite(number) ? number : 0;
}
