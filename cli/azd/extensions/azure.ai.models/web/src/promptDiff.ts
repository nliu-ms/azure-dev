export type PromptDiffLine = {
  kind: "context" | "added" | "removed";
  value: string;
};

const maxDiffCells = 1_000_000;

export function buildPromptDiff(source: string, optimized: string): PromptDiffLine[] {
  const before = source.split("\n");
  const after = optimized.split("\n");
  if (before.length * after.length > maxDiffCells) {
    return [
      ...before.map((value) => ({ kind: "removed" as const, value })),
      ...after.map((value) => ({ kind: "added" as const, value })),
    ];
  }

  const lengths = Array.from({ length: before.length + 1 }, () =>
    new Uint32Array(after.length + 1),
  );
  for (let beforeIndex = before.length - 1; beforeIndex >= 0; beforeIndex -= 1) {
    for (let afterIndex = after.length - 1; afterIndex >= 0; afterIndex -= 1) {
      lengths[beforeIndex][afterIndex] =
        before[beforeIndex] === after[afterIndex]
          ? lengths[beforeIndex + 1][afterIndex + 1] + 1
          : Math.max(
              lengths[beforeIndex + 1][afterIndex],
              lengths[beforeIndex][afterIndex + 1],
            );
    }
  }

  const result: PromptDiffLine[] = [];
  let beforeIndex = 0;
  let afterIndex = 0;
  while (beforeIndex < before.length && afterIndex < after.length) {
    if (before[beforeIndex] === after[afterIndex]) {
      result.push({ kind: "context", value: before[beforeIndex] });
      beforeIndex += 1;
      afterIndex += 1;
    } else if (
      lengths[beforeIndex + 1][afterIndex] >=
      lengths[beforeIndex][afterIndex + 1]
    ) {
      result.push({ kind: "removed", value: before[beforeIndex] });
      beforeIndex += 1;
    } else {
      result.push({ kind: "added", value: after[afterIndex] });
      afterIndex += 1;
    }
  }
  while (beforeIndex < before.length) {
    result.push({ kind: "removed", value: before[beforeIndex] });
    beforeIndex += 1;
  }
  while (afterIndex < after.length) {
    result.push({ kind: "added", value: after[afterIndex] });
    afterIndex += 1;
  }
  return result;
}
