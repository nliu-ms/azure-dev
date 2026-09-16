export type EvidenceSummary = {
  caseCount: number;
  caseIds: string[];
  sourceModel?: string;
  targetModel?: string;
  sourceRunId?: string;
  targetRunId?: string;
  stable?: number;
  regressions?: number;
  improvements?: number;
  promptSha256?: string;
  datasetSha256?: string;
  startedAt?: string;
  completedAt?: string;
};

function caseID(value: unknown): string {
  if (!value || typeof value !== "object") {
    return "";
  }
  const record = value as Record<string, unknown>;
  for (const key of ["case_id", "caseId", "id"]) {
    if (typeof record[key] === "string" && record[key]) {
      return record[key];
    }
  }
  return "";
}

function recordsFromJSON(value: unknown): unknown[] {
  if (Array.isArray(value)) {
    return value;
  }
  if (!value || typeof value !== "object") {
    return [];
  }
  const record = value as Record<string, unknown>;
  for (const key of ["cases", "results", "items", "records", "evaluations"]) {
    if (Array.isArray(record[key])) {
      return record[key] as unknown[];
    }
  }
  return [];
}

function declaredCaseCount(value: unknown): number {
  if (!value || typeof value !== "object") {
    return 0;
  }
  const record = value as Record<string, unknown>;
  for (const key of ["case_count", "caseCount", "total_cases", "totalCases"]) {
    if (typeof record[key] === "number" && record[key] >= 0) {
      return record[key];
    }
  }
  for (const key of ["summary", "aggregate", "baseline_evaluation"]) {
    const count = declaredCaseCount(record[key]);
    if (count > 0) {
      return count;
    }
  }
  return 0;
}

export function parseEvidence(content: string): EvidenceSummary {
  const trimmed = content.trim();
  if (!trimmed) {
    throw new Error("The file is empty.");
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    const records = trimmed
      .split(/\r?\n/)
      .filter((line) => line.trim())
      .map((line, index) => {
        try {
          return JSON.parse(line) as unknown;
        } catch {
          throw new Error(`Line ${index + 1} is not valid JSON.`);
        }
      });
    parsed = records;
  }

  const records = recordsFromJSON(parsed);
  const ids = Array.from(new Set(records.map(caseID).filter(Boolean)));
  const caseCount = records.length || declaredCaseCount(parsed);
  if (caseCount === 0) {
    throw new Error("No evaluation cases were found.");
  }
  const root =
    parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  const runs =
    root.runs && typeof root.runs === "object"
      ? (root.runs as Record<string, unknown>)
      : {};
  const source =
    runs.source && typeof runs.source === "object"
      ? (runs.source as Record<string, unknown>)
      : {};
  const target =
    runs.target && typeof runs.target === "object"
      ? (runs.target as Record<string, unknown>)
      : {};
  const summary =
    root.summary && typeof root.summary === "object"
      ? (root.summary as Record<string, unknown>)
      : {};
  const suite =
    root.suite && typeof root.suite === "object"
      ? (root.suite as Record<string, unknown>)
      : {};
  const sourceStart = typeof source.started_at === "string" ? source.started_at : "";
  const targetStart = typeof target.started_at === "string" ? target.started_at : "";
  const sourceEnd = typeof source.completed_at === "string" ? source.completed_at : "";
  const targetEnd = typeof target.completed_at === "string" ? target.completed_at : "";
  const starts = [sourceStart, targetStart].filter(Boolean).sort();
  const ends = [sourceEnd, targetEnd].filter(Boolean).sort();

  return {
    caseCount,
    caseIds: ids,
    sourceModel: typeof source.model === "string" ? source.model : undefined,
    targetModel: typeof target.model === "string" ? target.model : undefined,
    sourceRunId: typeof source.run_id === "string" ? source.run_id : undefined,
    targetRunId: typeof target.run_id === "string" ? target.run_id : undefined,
    stable: typeof summary.stable === "number" ? summary.stable : undefined,
    regressions:
      typeof summary.regression === "number" ? summary.regression : undefined,
    improvements:
      typeof summary.improvement === "number" ? summary.improvement : undefined,
    promptSha256:
      typeof suite.prompt_sha256 === "string" ? suite.prompt_sha256 : undefined,
    datasetSha256:
      typeof suite.dataset_sha256 === "string" ? suite.dataset_sha256 : undefined,
    startedAt: starts[0],
    completedAt: ends.at(-1),
  };
}

export function matchingCaseCount(dataset: EvidenceSummary, baseline: EvidenceSummary): number | null {
  if (dataset.caseIds.length === 0 || baseline.caseIds.length === 0) {
    return null;
  }
  const baselineIDs = new Set(baseline.caseIds);
  return dataset.caseIds.filter((id) => baselineIDs.has(id)).length;
}
