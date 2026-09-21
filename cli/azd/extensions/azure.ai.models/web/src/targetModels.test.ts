import { describe, expect, it } from "vitest";
import { matchingTargetDeployments, targetModelChoices, targetModelKey } from "./targetModels";
import type { ModelDeployment } from "./retirement";

const model = (name: string, deployment: string, format = "OpenAI", version = "1"): ModelDeployment => ({
  modelName: name, modelFormat: format, deploymentName: deployment, modelVersion: version,
  lifecycleStatus: "GenerallyAvailable", accountName: "account", accountKind: "AIServices",
  resourceGroup: "rg", location: "eastus", resourceId: `/accounts/account/deployments/${deployment}`,
});

describe("target model choices", () => {
  it("includes suggested, discovered and explicitly entered models without duplicates", () => {
    const choices = targetModelChoices(
      [model("gpt-5.2", "one"), model("gpt-5.2", "two", "OpenAI", "2")],
      { modelName: "gpt-5.4", modelFormat: "OpenAI" },
      { modelName: "custom-model", modelFormat: "Microsoft" },
    );
    expect(choices.map((choice) => choice.modelName)).toEqual(["custom-model", "gpt-5.2", "gpt-5.4"]);
  });

  it("uses model name and format, not the suggested model, to match deployments", () => {
    const source = model("gpt-4.1-mini", "source");
    const alternate = model("gpt-5.2", "alternate");
    expect(matchingTargetDeployments(
      [source, alternate, model("gpt-5.4", "default")],
      { modelName: "gpt-5.2", modelFormat: "OpenAI" }, source,
    )).toEqual([alternate]);
  });

  it("allows another version/deployment of the source model, but not the same deployment", () => {
    const source = model("gpt-4.1", "source");
    const other = model("gpt-4.1", "new-version", "OpenAI", "2");
    expect(matchingTargetDeployments([source, other], source, source)).toEqual([other]);
    expect(targetModelKey({ modelName: " GPT-4.1 ", modelFormat: "openai" })).toBe(targetModelKey(source));
  });
});
