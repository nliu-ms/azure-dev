import { describe, expect, it } from "vitest";
import { retirementState, type ModelDeployment } from "./retirement";

const baseModel: ModelDeployment = {
  deploymentName: "chat",
  modelName: "gpt-4o",
  modelVersion: "2024-05-13",
  modelFormat: "OpenAI",
  lifecycleStatus: "",
  accountName: "account",
  accountKind: "OpenAI",
  resourceGroup: "rg",
  location: "eastus",
  resourceId: "/deployments/chat",
};

describe("retirementState", () => {
  const now = Date.parse("2026-09-14T12:00:00Z");

  it("treats a timestamp earlier today as retired", () => {
    expect(
      retirementState(
        { ...baseModel, retirementDate: "2026-09-14T11:00:00Z" },
        now,
      ),
    ).toBe("retired");
  });

  it("keeps deprecating models visible when the date is missing", () => {
    expect(retirementState({ ...baseModel, lifecycleStatus: "Deprecating" }, now)).toBe(
      "upcoming",
    );
  });

  it("maps the ARM Deprecated state to retired", () => {
    expect(retirementState({ ...baseModel, lifecycleStatus: "Deprecated" }, now)).toBe(
      "retired",
    );
  });
});
