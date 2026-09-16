import { describe, expect, it } from "vitest";
import { buildPromptDiff } from "./promptDiff";

describe("buildPromptDiff", () => {
  it("marks inserted, removed, and unchanged lines", () => {
    expect(buildPromptDiff("Keep\nRemove\nEnd", "Keep\nAdd\nEnd")).toEqual([
      { kind: "context", value: "Keep" },
      { kind: "removed", value: "Remove" },
      { kind: "added", value: "Add" },
      { kind: "context", value: "End" },
    ]);
  });
});
