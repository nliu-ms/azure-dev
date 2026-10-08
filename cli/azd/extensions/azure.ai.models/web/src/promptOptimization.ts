import { appendMappedEvidence, type ConfirmedMapping } from "./evaluationMapping";

export type OptimizerDeployment = {
  accountName: string;
  modelName: string;
  deploymentName: string;
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
