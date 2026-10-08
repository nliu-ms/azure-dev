import { describe, expect, it } from "vitest";
import { createValidationForm, validationResultMatches, type ValidationResult } from "./validation";
import type { ConfirmedMapping, MappingLane } from "./evaluationMapping";

const lane: MappingLane = {
  file: 0, collection: "", filters: [], caseId: "/id", input: "/question", output: "/answer",
  model: "", runId: "", latencyMs: "", inputTokens: "", outputTokens: "",
  evaluator: { collection: "", filters: [], status: "/pass", score: "", rationale: "" },
  passRule: "reported", operator: "gte",
};
const evidence: ConfirmedMapping = {
  files: [new File(["baseline"], "baseline.csv")],
  mapping: { version: 1, adapter: "generic", source: lane, target: lane },
  preview: {
    valid: true, evaluationSha256: "b".repeat(64), caseCount: 4, comparableCount: 4,
    unclassifiedCount: 0, sourceOnly: 0, targetOnly: 0, issues: [], cases: [],
  },
};
const result: ValidationResult = {
  promptSha256: "a".repeat(64), baselineEvaluationSha256: "b".repeat(64),
  candidatePromptSha256: "c".repeat(64), adaptedTargetSha256: "d".repeat(64),
  caseCount: 4, resolvedCount: 1, remainingCount: 1, newFailureCount: 1, preservedCount: 1,
  unclassifiedCount: 0, missingCount: 0, extraCount: 0, readyForRolloutReview: false,
  resolvedCaseIds: ["a"], remainingCaseIds: ["b"], newFailureCaseIds: ["c"],
  unclassifiedCaseIds: [], issues: [], warnings: [],
};

describe("validation request", () => {
  it("sends the confirmed baseline, candidate prompt and one adapted Target file", () => {
    const form = createValidationForm(
      new File(["source"], "source.txt"), evidence, "source", "target",
      "candidate", new File(["adapted"], "target.csv"),
    );
    expect(form.getAll("files")).toHaveLength(1);
    expect(form.get("mappingConfirmed")).toBe("true");
    expect(form.get("evaluationSha256")).toBe("b".repeat(64));
    expect(form.get("candidatePrompt")).toBe("candidate");
    expect((form.get("adaptedTarget") as File).name).toBe("target.csv");
  });

  it("rejects stale or malformed validation responses", () => {
    expect(validationResultMatches(result, "a".repeat(64), "b".repeat(64), 4)).toBe(true);
    expect(validationResultMatches(result, "e".repeat(64), "b".repeat(64), 4)).toBe(false);
    expect(validationResultMatches(result, "a".repeat(64), "e".repeat(64), 4)).toBe(false);
    expect(validationResultMatches({ ...result, candidatePromptSha256: "" }, "a".repeat(64), "b".repeat(64), 4))
      .toBe(false);
  });
});
