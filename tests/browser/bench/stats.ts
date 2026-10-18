export interface Stat {
  n: number;
  mean: number;
  p50: number;
  p95: number;
  p99: number;
  max: number;
}

/** Nearest-rank percentiles; with few samples p99 equals the maximum, which the report states. */
export function stat(samples: number[]): Stat {
  const s = [...samples].sort((a, b) => a - b);
  const rank = (p: number): number => s[Math.min(s.length - 1, Math.max(0, Math.ceil((p / 100) * s.length) - 1))] ?? 0;
  const round = (v: number): number => Math.round(v * 1000) / 1000;
  return {
    n: s.length,
    mean: round(s.reduce((a, b) => a + b, 0) / Math.max(1, s.length)),
    p50: round(rank(50)),
    p95: round(rank(95)),
    p99: round(rank(99)),
    max: round(s.at(-1) ?? 0),
  };
}
