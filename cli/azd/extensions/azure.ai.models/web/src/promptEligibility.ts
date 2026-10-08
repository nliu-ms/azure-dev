export type ComparisonFinding = {
  outcome: "regression" | "pre_existing_failure" | "operational_regression";
};

export function optimizationEvidenceCounts(cases: readonly ComparisonFinding[]) {
  return {
    caseCount: cases.length,
    regressionCount: cases.filter((item) => item.outcome === "regression").length,
    residualFailureCount: cases.filter((item) => item.outcome === "pre_existing_failure").length,
    operationalCount: cases.filter((item) => item.outcome === "operational_regression").length,
  };
}
