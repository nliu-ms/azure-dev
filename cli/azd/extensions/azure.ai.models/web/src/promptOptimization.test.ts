import { describe, expect, it } from "vitest";
import { createOptimizationForm } from "./promptOptimization";
import type { ConfirmedMapping, MappingLane } from "./evaluationMapping";

describe("optimization request", () => {
  it("includes the confirmed evidence and optimizer without implicit disclosure consent", () => {
    const lane: MappingLane = {
      file: 0, collection: "", filters: [], caseId: "/id", input: "/question", output: "/answer",
      model: "", runId: "", latencyMs: "", inputTokens: "", outputTokens: "",
      evaluator: { collection: "", filters: [], status: "/pass", score: "", rationale: "" },
      passRule: "reported", operator: "gte",
    };
    const evidence: ConfirmedMapping = {
      files: [new File(["test"], "results.csv")],
      mapping: { version: 1, adapter: "generic", source: lane, target: lane },
      preview: {
        valid: true, evaluationSha256: "b".repeat(64), caseCount: 3,
        comparableCount: 3, unclassifiedCount: 0, sourceOnly: 0, targetOnly: 0, issues: [], cases: [],
      },
    };
    const form = createOptimizationForm(
      new File(["Source prompt"], "prompt.txt"),
      evidence,
      "source",
      "target",
      { accountName: "test-account", modelName: "gpt-5.2", deploymentName: "optimizer" },
    );
    expect(form.get("optimizerAccountName")).toBe("test-account");
    expect(form.get("mappingConfirmed")).toBe("true");
    expect(form.get("evaluationSha256")).toBe(evidence.preview.evaluationSha256);
    expect(form.has("allowEvaluationContent")).toBe(false);
    expect(form.has("allowAI")).toBe(false);
  });
});
