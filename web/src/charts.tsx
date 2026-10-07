export interface Ticks {
  values: number[];
  step: number;
}

// (max / step) * step can leave floating-point dust, e.g. 4.999999999999999.
function round(v: number): number {
  return Math.round(v * 1e6) / 1e6;
}

export function niceTicks(max: number, targetCount = 4): Ticks {
  if (!(max > 0)) return { values: [0], step: 1 };
  const roughStep = max / targetCount;
  const magnitude = 10 ** Math.floor(Math.log10(roughStep));
  const residual = roughStep / magnitude;
  const rung = residual <= 1 ? 1 : residual <= 2 ? 2 : residual <= 5 ? 5 : 10;
  const step = rung * magnitude;
  const top = Math.ceil(max / step) * step;
  const values: number[] = [];
  for (let v = 0; v <= top + step / 2; v += step) values.push(round(v));
  return { values, step };
}

export function rollingMedian(values: number[], window = 10): number[] {
  return values.map((_, i) => {
    const slice = values.slice(Math.max(0, i - window + 1), i + 1).sort((a, b) => a - b);
    const mid = Math.floor(slice.length / 2);
    return slice.length % 2 === 0 ? (slice[mid - 1] + slice[mid]) / 2 : slice[mid];
  });
}
