import { describe, expect, it } from "vitest";
import { optimizationEvidenceCounts } from "./promptEligibility";

describe("optimization evidence scope", () => {
  it("includes all findings independently of legacy fixability labels", () => {
    const cases = [
      { outcome: "regression", promptFixable: "unknown" },
      { outcome: "pre_existing_failure", promptFixable: "no" },
      { outcome: "operational_regression", promptFixable: "no" },
      { outcome: "regression", promptFixable: "candidate" },
    ] as const;
    expect(optimizationEvidenceCounts(cases)).toEqual({
      caseCount: 4, regressionCount: 2, residualFailureCount: 1, operationalCount: 1,
    });
  });

  it("does not cap evidence at twenty cases", () => {
    const cases = Array.from({ length: 851 }, (_, i) => ({
      outcome: i < 567 ? "regression" as const : "pre_existing_failure" as const,
    }));
    expect(optimizationEvidenceCounts(cases)).toEqual({
      caseCount: 851, regressionCount: 567, residualFailureCount: 284, operationalCount: 0,
    });
  });

  it("permits a zero-finding scope for general prompt improvement", () => {
    expect(optimizationEvidenceCounts([])).toEqual({
      caseCount: 0, regressionCount: 0, residualFailureCount: 0, operationalCount: 0,
    });
  });
});
