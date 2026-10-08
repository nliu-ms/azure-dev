export type AdaptStep = "monitor" | "evaluation" | "optimization";

export const adaptSteps: { id: AdaptStep; label: string }[] = [
  { id: "monitor", label: "Azure Monitor" },
  { id: "evaluation", label: "Evaluation data" },
  { id: "optimization", label: "Prompt optimization" },
];

export function canVisitAdaptStep(step: AdaptStep, monitorComplete: boolean, evaluationComplete: boolean): boolean {
  if (step === "monitor") return true;
  if (step === "evaluation") return monitorComplete;
  return monitorComplete && evaluationComplete;
}
