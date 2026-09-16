import { describe, expect, it } from "vitest";
import { matchingCaseCount, parseEvidence } from "./evidence";

describe("parseEvidence", () => {
  it("parses JSONL datasets and preserves unique case IDs", () => {
    expect(parseEvidence('{"case_id":"a"}\n{"case_id":"b"}\n')).toEqual({
      caseCount: 2,
      caseIds: ["a", "b"],
    });
  });

  it("reads result arrays and declared aggregate counts", () => {
    expect(parseEvidence('{"results":[{"caseId":"a"},{"caseId":"b"}]}').caseCount).toBe(2);
    expect(
      parseEvidence(
        '{"suite":{"prompt_sha256":"prompt-hash","dataset_sha256":"dataset-hash"},' +
          '"runs":{"source":{"run_id":"source-1","model":"gpt-4o","started_at":"2026-09-14T01:00:00Z"},' +
          '"target":{"run_id":"target-1","model":"gpt-5.4","completed_at":"2026-09-14T02:00:00Z"}},' +
          '"summary":{"total_cases":20,"stable":14,"regression":4,"improvement":2}}',
      ),
    ).toMatchObject({
      caseCount: 20,
      sourceModel: "gpt-4o",
      targetModel: "gpt-5.4",
      sourceRunId: "source-1",
      targetRunId: "target-1",
      stable: 14,
      regressions: 4,
      improvements: 2,
      promptSha256: "prompt-hash",
      datasetSha256: "dataset-hash",
      startedAt: "2026-09-14T01:00:00Z",
      completedAt: "2026-09-14T02:00:00Z",
    });
  });

  it("rejects invalid or empty evidence", () => {
    expect(() => parseEvidence("")).toThrow("empty");
    expect(() => parseEvidence('{"summary":{}}')).toThrow("No evaluation cases");
  });
});

describe("matchingCaseCount", () => {
  it("matches dataset and result case IDs", () => {
    expect(
      matchingCaseCount(
        { caseCount: 3, caseIds: ["a", "b", "c"] },
        { caseCount: 2, caseIds: ["b", "c"] },
      ),
    ).toBe(2);
  });

  it("returns null when either artifact has no case IDs", () => {
    expect(
      matchingCaseCount(
        { caseCount: 20, caseIds: [] },
        { caseCount: 20, caseIds: [] },
      ),
    ).toBeNull();
  });
});
