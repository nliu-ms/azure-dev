import { appendMappedEvidence, type ConfirmedMapping } from "./evaluationMapping";
import { optimizationEvidenceCounts, type ComparisonFinding } from "./promptEligibility";

export type OptimizerDeployment = {
  accountName: string;
  modelName: string;
  deploymentName: string;
};

export type OptimizationRequestBody = {
  developer_message: string;
  messages: unknown[];
  model_name: string;
  model_deployment_name: string;
  optimizing_for: string;
  requested_changes: string;
  tools: unknown[];
};

export type OptimizationPreview = {
  promptSha256: string;
  evaluationSha256: string;
  requestSha256: string;
  caseCount: number;
  regressionCount: number;
  residualFailureCount: number;
  operationalCount: number;
  requestBytes: number;
  maxRequestBytes: number;
  withinLimit: boolean;
  request: OptimizationRequestBody | null;
  warnings: string[];
};

export type OptimizationAnalysis = {
  promptSha256: string;
  evaluationSha256: string;
  cases: readonly ComparisonFinding[];
};

export function createOptimizationForm(
  prompt: File,
  evidence: ConfirmedMapping,
  sourceModel: string,
  targetModel: string,
  optimizer: OptimizerDeployment,
): FormData {
  const form = new FormData();
  form.append("prompt", prompt, prompt.name);
  appendMappedEvidence(form, evidence);
  form.append("sourceModelName", sourceModel);
  form.append("targetModelName", targetModel);
  form.append("optimizerAccountName", optimizer.accountName);
  form.append("optimizerModelName", optimizer.modelName);
  form.append("optimizerDeploymentName", optimizer.deploymentName);
  return form;
}

export function validateOptimizationPreview(
  preview: OptimizationPreview,
  analysis: OptimizationAnalysis,
  optimizer?: OptimizerDeployment,
): void {
  if (preview.promptSha256 !== analysis.promptSha256 ||
      preview.evaluationSha256 !== analysis.evaluationSha256 ||
      !/^[a-f0-9]{64}$/.test(preview.requestSha256)) {
    throw new Error("The optimization preview does not match the current evidence. Prepare it again.");
  }
  const expected = optimizationEvidenceCounts(analysis.cases);
  if (preview.caseCount !== expected.caseCount || preview.regressionCount !== expected.regressionCount ||
      preview.residualFailureCount !== expected.residualFailureCount ||
      preview.operationalCount !== expected.operationalCount) {
    throw new Error("The optimization preview omitted or reclassified findings. No content was sent to the optimizer.");
  }
  if (!Number.isSafeInteger(preview.requestBytes) || preview.requestBytes < 0 ||
      !Number.isSafeInteger(preview.maxRequestBytes) || preview.maxRequestBytes <= 0 ||
      preview.withinLimit !== (preview.requestBytes <= preview.maxRequestBytes) ||
      (preview.withinLimit && (preview.request == null || typeof preview.request !== "object" ||
        typeof preview.request.developer_message !== "string" || typeof preview.request.requested_changes !== "string")) ||
      (!preview.withinLimit && preview.request !== null)) {
    throw new Error("The optimization preview returned an inconsistent request size.");
  }
  if (optimizer && preview.request &&
      (preview.request.model_name !== optimizer.modelName ||
       preview.request.model_deployment_name !== optimizer.deploymentName)) {
    throw new Error("The optimization preview does not match the selected optimizer deployment.");
  }
}
