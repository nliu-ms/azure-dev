import type { ModelDeployment } from "./retirement";

export type TargetModelChoice = {
  modelName: string;
  modelFormat: string;
};

export function targetModelKey(model: TargetModelChoice): string {
  return JSON.stringify([model.modelFormat.trim().toLowerCase(), model.modelName.trim().toLowerCase()]);
}

export function targetModelChoices(
  deployments: readonly ModelDeployment[],
  suggested: TargetModelChoice | null,
  selected: TargetModelChoice | null,
): TargetModelChoice[] {
  const choices = new Map<string, TargetModelChoice>();
  for (const choice of [suggested, selected, ...deployments]) {
    if (!choice?.modelName.trim() || !choice.modelFormat.trim()) continue;
    const normalized = { modelName: choice.modelName.trim(), modelFormat: choice.modelFormat.trim() };
    const key = targetModelKey(normalized);
    if (!choices.has(key)) choices.set(key, normalized);
  }
  return [...choices.values()].sort((left, right) =>
    left.modelName.localeCompare(right.modelName) || left.modelFormat.localeCompare(right.modelFormat),
  );
}

export function matchingTargetDeployments(
  deployments: readonly ModelDeployment[],
  target: TargetModelChoice | null,
  source: ModelDeployment | null,
): ModelDeployment[] {
  if (!target) return [];
  const sourceId = source
    ? (source.resourceId || `${source.accountName}/${source.deploymentName}`).toLowerCase() : "";
  return deployments.filter((model) =>
    targetModelKey(model) === targetModelKey(target) &&
    (model.resourceId || `${model.accountName}/${model.deploymentName}`).toLowerCase() !== sourceId,
  );
}
