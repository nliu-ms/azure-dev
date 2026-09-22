export type MappingField = {
  path: string;
  types: string[];
  present: number;
  distinct: number;
  sample?: string;
};

export type MappingCollection = {
  path: string;
  rowCount: number;
  fields: MappingField[];
};

export type MappingFileProfile = {
  index: number;
  name: string;
  sha256: string;
  format: string;
  collections: MappingCollection[];
};

export type MappingFilter = { path: string; value: string };

export type MappingEvaluator = {
  collection: string;
  filters: MappingFilter[];
  status: string;
  score: string;
  rationale: string;
};

export type MappingLane = {
  file: number;
  collection: string;
  filters: MappingFilter[];
  caseId: string;
  input: string;
  output: string;
  model: string;
  runId: string;
  latencyMs: string;
  inputTokens: string;
  outputTokens: string;
  context?: string;
  expectedOutput?: string;
  expectedReferences?: string;
  evaluator: MappingEvaluator;
  passRule: "reported" | "threshold";
  operator: "gte" | "lte";
  threshold?: number;
};

export type DatasetMapping = {
  file: number;
  collection: string;
  caseId: string;
  input: string;
  reference: string;
  expectedOutput?: string;
};

export type EvaluationMapping = {
  version: 1;
  adapter: "generic" | "canonical" | "meera" | "foundry";
  source: MappingLane;
  target: MappingLane;
  dataset?: DatasetMapping;
};

export type MappingProfile = {
  files: MappingFileProfile[];
  mapping: EvaluationMapping;
  warnings: string[];
};

export type MappingIssue = {
  severity: "error" | "warning";
  code: string;
  message: string;
  caseId?: string;
};

export type MappingPreviewCase = {
  caseId: string;
  input: string;
  sourceOutput: string;
  targetOutput: string;
  sourceStatus: string;
  targetStatus: string;
  sourceScore?: number;
  targetScore?: number;
};

export type MappingPreview = {
  valid: boolean;
  evaluationSha256: string;
  caseCount: number;
  comparableCount: number;
  unclassifiedCount: number;
  sourceOnly: number;
  targetOnly: number;
  issues: MappingIssue[];
  cases: MappingPreviewCase[];
  sourceModel?: string;
  targetModel?: string;
  sourceRunId?: string;
  targetRunId?: string;
  startedAt?: string;
  completedAt?: string;
};

export type ConfirmedMapping = {
  files: File[];
  mapping: EvaluationMapping;
  preview: MappingPreview;
};

export type MappingDraft = {
  files: File[];
  profile: MappingProfile | null;
  mapping: EvaluationMapping | null;
  preview: MappingPreview | null;
  acknowledged: boolean;
  confirmed: boolean;
  origin: string;
  contextKey: string;
};

export function createMappingDraft(): MappingDraft {
  return {
    files: [], profile: null, mapping: null, preview: null,
    acknowledged: false, confirmed: false, origin: "Detected locally", contextKey: "",
  };
}

export const maxEvaluationBytes = 24 * 1024 * 1024;
export const maxEvaluationFiles = 3;

export function validateEvaluationFiles(
  files: Pick<File, "name" | "size">[],
  promptBytes = 0,
): string | null {
  if (files.length === 0 || files.length > maxEvaluationFiles) {
    return "Choose between 1 and 3 evaluation files. The Source prompt is uploaded separately.";
  }
  if (files.some((file) => file.size === 0)) {
    return "Empty files cannot be added. Choose an evaluation file containing records.";
  }
  if (files.some((file) => !/\.(jsonl?|csv|xlsx)$/i.test(file.name))) {
    return "Use JSON, JSONL, CSV, or XLSX evaluation files.";
  }
  if (files.reduce((total, file) => total + file.size, promptBytes) >= maxEvaluationBytes) {
    return "The Source prompt and evaluation files must total less than 24 MB.";
  }
  return null;
}

export function appendMappedEvidence(form: FormData, evidence: ConfirmedMapping): void {
  for (const file of evidence.files) {
    form.append("files", file, file.name);
  }
  form.append("mapping", JSON.stringify(evidence.mapping));
  form.append("mappingConfirmed", "true");
  form.append("evaluationSha256", evidence.preview.evaluationSha256);
}

export function canConfirmMapping(preview: MappingPreview, acknowledged: boolean): boolean {
  return (
    acknowledged &&
    preview.valid &&
    preview.comparableCount > 0 &&
    preview.sourceOnly === 0 &&
    preview.targetOnly === 0 &&
    !preview.issues.some((issue) => issue.severity === "error") &&
    /^[a-f0-9]{64}$/.test(preview.evaluationSha256)
  );
}

export function mappingSchemaPayload(profile: MappingProfile) {
  return {
    files: profile.files.map((file) => ({
      index: file.index,
      collections: file.collections.map((collection, index) => ({
        id: `collection:${index}`,
        rowCount: collection.rowCount,
        fields: collection.fields.map((field) => ({
          path: field.path,
          types: field.types,
          present: field.present,
          distinct: field.distinct,
        })),
      })),
    })),
  };
}

export function fieldsForLane(profile: MappingProfile, lane: MappingLane): MappingField[] {
  return (
    profile.files
      .find((file) => file.index === lane.file)
      ?.collections.find((collection) => collection.path === lane.collection)?.fields ?? []
  );
}
