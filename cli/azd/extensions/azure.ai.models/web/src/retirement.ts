export type ModelDeployment = {
  deploymentName: string;
  modelName: string;
  modelVersion: string;
  modelFormat: string;
  lifecycleStatus: string;
  retirementDate?: string;
  versionUpgradeOption?: string;
  accountName: string;
  accountKind: string;
  resourceGroup: string;
  location: string;
  resourceId: string;
};

export type RetirementState = "retired" | "critical" | "upcoming" | "healthy" | "unknown";

export function retirementState(
  model: ModelDeployment,
  now = Date.now(),
): RetirementState {
  const lifecycle = model.lifecycleStatus.toLowerCase();
  if (lifecycle === "deprecated") {
    return "retired";
  }

  if (model.retirementDate) {
    const retirement = new Date(model.retirementDate).getTime();
    if (!Number.isNaN(retirement)) {
      if (retirement <= now) {
        return "retired";
      }
      const days = daysUntil(model.retirementDate, now);
      if (days <= 90) {
        return "critical";
      }
      if (days <= 365) {
        return "upcoming";
      }
      return lifecycle === "deprecating" ? "upcoming" : "healthy";
    }
  }

  return lifecycle === "deprecating" ? "upcoming" : "unknown";
}

export function daysUntil(value: string, now = Date.now()): number {
  const date = new Date(value);
  return Math.ceil((date.getTime() - now) / 86_400_000);
}

export function formatDate(value?: string): string {
  if (!value) {
    return "Not published";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat("en", {
    year: "numeric",
    month: "short",
    day: "numeric",
  }).format(date);
}

export function statusLabel(model: ModelDeployment): string {
  const state = retirementState(model);
  if (state === "retired") {
    return "Retired";
  }
  if (state === "critical") {
    return `${daysUntil(model.retirementDate!)} days left`;
  }
  if (model.lifecycleStatus.toLowerCase() === "deprecating") {
    return "Deprecated";
  }
  if (state === "upcoming") {
    return "Migration recommended";
  }
  if (model.lifecycleStatus) {
    return model.lifecycleStatus;
  }
  return "Date unavailable";
}
