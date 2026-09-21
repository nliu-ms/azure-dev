import { describe, expect, it } from "vitest";
import { canVisitAdaptStep } from "./adaptWorkflow";
import { uniqueEvidenceNotes } from "./EvidenceNotes";

describe("Adapt step navigation", () => {
  it("requires a Monitor result or explicit skip before evaluation", () => {
    expect(canVisitAdaptStep("monitor", false, false)).toBe(true);
    expect(canVisitAdaptStep("evaluation", false, false)).toBe(false);
    expect(canVisitAdaptStep("evaluation", true, false)).toBe(true);
  });

  it("requires current analysis and an acknowledged Monitor step before optimization", () => {
    expect(canVisitAdaptStep("optimization", true, false)).toBe(false);
    expect(canVisitAdaptStep("optimization", false, true)).toBe(false);
    expect(canVisitAdaptStep("optimization", true, true)).toBe(true);
  });
});

describe("quiet evidence notes", () => {
  it("deduplicates without removing provenance or limitations", () => {
    expect(uniqueEvidenceNotes([
      "Synthetic outputs.", " Synthetic outputs. ", "", "Context is unavailable.",
    ])).toEqual(["Synthetic outputs.", "Context is unavailable."]);
  });
});
