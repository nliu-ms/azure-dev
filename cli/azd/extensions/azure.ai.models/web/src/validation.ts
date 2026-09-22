import { appendMappedEvidence, type ConfirmedMapping, type MappingIssue } from "./evaluationMapping";

export type ValidationResult = {
  promptSha256: string;
  baselineEvaluationSha256: string;
  candidatePromptSha256: string;
  adaptedTargetSha256: string;
  caseCount: number;
  resolvedCount: number;
  remainingCount: number;
  newFailureCount: number;
  preservedCount: number;
  unclassifiedCount: number;
  missingCount: number;
  extraCount: number;
  readyForRolloutReview: boolean;
  resolvedCaseIds: string[];
  remainingCaseIds: string[];
  newFailureCaseIds: string[];
  unclassifiedCaseIds: string[];
  issues: MappingIssue[];
  warnings: string[];
};

export function createValidationForm(
  sourcePrompt: File,
  evidence: ConfirmedMapping,
  sourceModel: string,
  targetModel: string,
  candidatePrompt: string,
  adaptedTarget: File,
): FormData {
  const form = new FormData();
  form.append("prompt", sourcePrompt, sourcePrompt.name);
  appendMappedEvidence(form, evidence);
  form.append("sourceModelName", sourceModel);
  form.append("targetModelName", targetModel);
  form.append("candidatePrompt", candidatePrompt);
  form.append("adaptedTarget", adaptedTarget, adaptedTarget.name);
  return form;
}

export function validationResultMatches(
  result: ValidationResult,
  promptSha256: string,
  evaluationSha256: string,
  caseCount: number,
): boolean {
  return result.promptSha256 === promptSha256 &&
    result.baselineEvaluationSha256 === evaluationSha256 &&
    result.caseCount === caseCount &&
    /^[a-f0-9]{64}$/.test(result.candidatePromptSha256) &&
    /^[a-f0-9]{64}$/.test(result.adaptedTargetSha256);
}
