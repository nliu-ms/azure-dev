import { describe, expect, it } from "vitest";
import {
  appendMappedEvidence,
  canConfirmMapping,
  fieldsForLane,
  mappingSchemaPayload,
  maxEvaluationBytes,
  validateEvaluationFiles,
  type EvaluationMapping,
  type MappingLane,
  type MappingPreview,
  type MappingProfile,
} from "./evaluationMapping";

const lane: MappingLane = {
  file: 0,
  collection: "",
  filters: [],
  caseId: "/id",
  input: "/question",
  output: "/answer",
  model: "",
  runId: "",
  latencyMs: "",
  inputTokens: "",
  outputTokens: "",
  evaluator: { collection: "", filters: [], status: "/pass", score: "", rationale: "" },
  passRule: "reported",
  operator: "gte",
};
const mapping: EvaluationMapping = {
  version: 1,
  adapter: "generic",
  source: lane,
  target: { ...lane, file: 1 },
};
const preview: MappingPreview = {
  valid: true,
  evaluationSha256: "a".repeat(64),
  caseCount: 2,
  comparableCount: 2,
  unclassifiedCount: 0,
  sourceOnly: 0,
  targetOnly: 0,
  issues: [],
  cases: [],
};

describe("evaluation file intake", () => {
  it.each([1, 2, 3])("accepts %i files without requiring a layout tab", (count) => {
    const files = Array.from({ length: count }, (_, i) => ({ name: `run-${i}.jsonl`, size: 100 }));
    expect(validateEvaluationFiles(files)).toBeNull();
  });

  it.each([
    { files: [] },
    { files: Array.from({ length: 4 }, () => ({ name: "run.json", size: 10 })) },
    { files: [{ name: "empty.csv", size: 0 }] },
    { files: [{ name: "config.yaml", size: 10 }] },
  ])("rejects invalid file selection $files", ({ files }) => {
    expect(validateEvaluationFiles(files)).not.toBeNull();
  });

  it("accepts mixed containers and uppercase extensions", () => {
    expect(validateEvaluationFiles([
      { name: "source.JSON", size: 10 },
      { name: "target.csv", size: 10 },
      { name: "dataset.xlsx", size: 10 },
    ])).toBeNull();
  });

  it("counts the separately uploaded prompt in the total byte limit", () => {
    expect(validateEvaluationFiles([{ name: "run.json", size: maxEvaluationBytes - 1 }], 1))
      .toContain("24 MB");
  });
});

describe("reviewed mapping contract", () => {
  it("requires explicit confirmation of a valid comparable preview", () => {
    expect(canConfirmMapping(preview, false)).toBe(false);
    expect(canConfirmMapping(preview, true)).toBe(true);
    expect(canConfirmMapping({ ...preview, comparableCount: 0 }, true)).toBe(false);
    expect(canConfirmMapping({ ...preview, valid: false }, true)).toBe(false);
    expect(canConfirmMapping({ ...preview, evaluationSha256: "" }, true)).toBe(false);
  });

  it("does not let unmatched cases or blocking issues bypass confirmation", () => {
    expect(canConfirmMapping({ ...preview, sourceOnly: 1 }, true)).toBe(false);
    expect(canConfirmMapping({ ...preview, targetOnly: 1 }, true)).toBe(false);
    expect(canConfirmMapping({
      ...preview, issues: [{ severity: "error", code: "DUPLICATE_KEY", message: "Duplicate case ID" }],
    }, true)).toBe(false);
  });

  it("retains acknowledged unknown grades without turning them into failures", () => {
    expect(canConfirmMapping({
      ...preview,
      comparableCount: 1,
      unclassifiedCount: 1,
      issues: [{ severity: "warning", code: "UNKNOWN_GRADE", message: "One unknown grade" }],
    }, true)).toBe(true);
  });

  it("sends identical originals, mapping and confirmed digest to analysis and optimization", () => {
    const files = [
      new File(['{"id":1}'], "source.json", { type: "application/json" }),
      new File(['{"id":1}'], "target.json", { type: "application/json" }),
    ];
    const form = new FormData();
    appendMappedEvidence(form, { files, mapping, preview });
    expect(form.getAll("files").map((file) => typeof file === "string" ? file : file.name))
      .toEqual(["source.json", "target.json"]);
    expect(JSON.parse(String(form.get("mapping")))).toEqual(mapping);
    expect(form.get("evaluationSha256")).toBe(preview.evaluationSha256);
    expect(form.get("mappingConfirmed")).toBe("true");
    expect(form.has("evaluation")).toBe(false);
  });
});

describe("local field profile", () => {
  const profile: MappingProfile = {
    files: [{
      index: 0,
      name: "confidential-customer-name.json",
      sha256: "private-file-digest",
      format: "json",
      collections: [{
        path: "",
        rowCount: 3,
        fields: [{
          path: "/sample.output_text", types: ["string"], present: 3, distinct: 3, sample: "private answer",
        }],
      }],
    }],
    mapping,
    warnings: ["private sample warning"],
  };

  it("shows a structural AI payload with counts but without filenames, samples, digests or rules", () => {
    const payload = mappingSchemaPayload(profile);
    const text = JSON.stringify(payload);
    expect(text).toContain("/sample.output_text");
    expect(text).not.toContain("private");
    expect(text).not.toContain("confidential");
    expect(text).not.toContain("mapping");
    expect(text).not.toContain("sample\":");
    expect(text).toContain("\"rowCount\":3");
    expect(text).toContain("\"distinct\":3");
    expect(text).not.toContain("format");
    expect(payload.files[0].collections[0].id).toBe("collection:0");
  });

  it("uses exact collection and literal dotted field paths", () => {
    expect(fieldsForLane(profile, lane)[0].path).toBe("/sample.output_text");
    expect(fieldsForLane(profile, { ...lane, file: 1 })).toEqual([]);
    expect(fieldsForLane(profile, { ...lane, collection: "/missing" })).toEqual([]);
  });
});
