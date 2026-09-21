import { describe, expect, it } from "vitest";
import {
  createOptimizationForm, validateOptimizationPreview,
  type OptimizationPreview,
} from "./promptOptimization";
import type { ConfirmedMapping, MappingLane } from "./evaluationMapping";

const optimizer = { accountName: "test-account", modelName: "gpt-5.2", deploymentName: "optimizer" };
const analysis = {
  promptSha256: "a".repeat(64), evaluationSha256: "b".repeat(64),
  cases: [
    { outcome: "regression" as const },
    { outcome: "pre_existing_failure" as const },
    { outcome: "operational_regression" as const },
  ],
};
const preview: OptimizationPreview = {
  promptSha256: analysis.promptSha256,
  evaluationSha256: analysis.evaluationSha256,
  requestSha256: "c".repeat(64),
  caseCount: 3, regressionCount: 1, residualFailureCount: 1, operationalCount: 1,
  requestBytes: 1000, maxRequestBytes: 2 * 1024 * 1024, withinLimit: true,
  request: {
    developer_message: "Source prompt", messages: [],
    model_name: "gpt-5.2", model_deployment_name: "optimizer", optimizing_for: "gpt-5.2",
    requested_changes: "Ground proposed changes in the supplied evidence.", tools: [],
  },
  warnings: [],
};

describe("optimization payload review", () => {
  it("accepts a complete matching scope and a prompt-only scope", () => {
    expect(() => validateOptimizationPreview(preview, analysis, optimizer)).not.toThrow();
    expect(() => validateOptimizationPreview({
      ...preview, caseCount: 0, regressionCount: 0, residualFailureCount: 0, operationalCount: 0,
    }, { ...analysis, cases: [] }, optimizer)).not.toThrow();
  });

  it("rejects silent filtering, reclassification and stale evidence", () => {
    expect(() => validateOptimizationPreview({ ...preview, caseCount: 2 }, analysis)).toThrow("omitted");
    expect(() => validateOptimizationPreview({ ...preview, regressionCount: 0 }, analysis)).toThrow("reclassified");
    expect(() => validateOptimizationPreview({ ...preview, promptSha256: "d".repeat(64) }, analysis)).toThrow("current evidence");
    expect(() => validateOptimizationPreview({ ...preview, evaluationSha256: "e".repeat(64) }, analysis)).toThrow("current evidence");
    expect(() => validateOptimizationPreview({ ...preview, requestSha256: "" }, analysis)).toThrow("current evidence");
  });

  it("accepts an explicitly oversized preview without a partial request", () => {
    expect(() => validateOptimizationPreview({
      ...preview, requestBytes: preview.maxRequestBytes + 1, withinLimit: false, request: null,
    }, analysis)).not.toThrow();
    expect(() => validateOptimizationPreview({
      ...preview, requestBytes: preview.maxRequestBytes + 1,
    }, analysis)).toThrow("request size");
    expect(() => validateOptimizationPreview({
      ...preview, requestBytes: preview.maxRequestBytes + 1, withinLimit: false,
    }, analysis)).toThrow("request size");
    expect(() => validateOptimizationPreview({ ...preview, request: null }, analysis)).toThrow("request size");
  });

  it("binds the displayed request to the chosen optimizer", () => {
    expect(() => validateOptimizationPreview(preview, analysis, { ...optimizer, deploymentName: "different" }))
      .toThrow("optimizer deployment");
  });

  it("local preparation does not implicitly authorize content disclosure", () => {
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
        valid: true, evaluationSha256: analysis.evaluationSha256, caseCount: 3,
        comparableCount: 3, unclassifiedCount: 0, sourceOnly: 0, targetOnly: 0, issues: [], cases: [],
      },
    };
    const form = createOptimizationForm(
      new File(["Source prompt"], "prompt.txt"), evidence, "source", "target", optimizer,
    );
    expect(form.get("optimizerAccountName")).toBe("test-account");
    expect(form.get("mappingConfirmed")).toBe("true");
    expect(form.get("evaluationSha256")).toBe(analysis.evaluationSha256);
    expect(form.has("allowEvaluationContent")).toBe(false);
    expect(form.has("allowAI")).toBe(false);
    expect(form.has("optimizationRequestSha256")).toBe(false);
  });
});
