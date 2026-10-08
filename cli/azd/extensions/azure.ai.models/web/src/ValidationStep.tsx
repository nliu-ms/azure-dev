import { useRef, useState } from "react";
import { Badge, Button, MessageBar, MessageBarBody, Spinner } from "@fluentui/react-components";
import type { ConfirmedMapping } from "./evaluationMapping";
import { EvidenceNotes } from "./EvidenceNotes";
import { createValidationForm, validationResultMatches, type ValidationResult } from "./validation";

type Props = {
  token: string;
  sourcePrompt: { file: File; sha256: string };
  evidence: ConfirmedMapping;
  sourceModel: string;
  targetModel: string;
  candidatePrompt: string;
  baselineCaseCount: number;
  result: ValidationResult | null;
  onResult: (result: ValidationResult | null) => void;
};

export function ValidationStep({
  token, sourcePrompt, evidence, sourceModel, targetModel, candidatePrompt,
  baselineCaseCount, result, onResult,
}: Props) {
  const inputRef = useRef<HTMLInputElement>(null);
  const requestId = useRef(0);
  const [file, setFile] = useState<File | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const validate = async () => {
    if (!file) return;
    const id = ++requestId.current;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 90_000);
    setLoading(true);
    setError("");
    onResult(null);
    try {
      const response = await fetch("/api/evaluation-validation", {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
        body: createValidationForm(
          sourcePrompt.file, evidence, sourceModel, targetModel, candidatePrompt, file,
        ),
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error ?? `Validation failed (${response.status}).`);
      const next = payload as ValidationResult;
      if (!validationResultMatches(
        next, sourcePrompt.sha256, evidence.preview.evaluationSha256, baselineCaseCount,
      )) throw new Error("The validation result does not match the current prompt and evidence.");
      if (id === requestId.current) onResult(next);
    } catch (failure) {
      if (id === requestId.current) {
        setError(failure instanceof DOMException && failure.name === "AbortError"
          ? "Validation timed out." : failure instanceof Error ? failure.message : String(failure));
      }
    } finally {
      window.clearTimeout(timeout);
      if (id === requestId.current) setLoading(false);
    }
  };

  return (
    <section className="lifecycle-card validation-step">
      <div className="lifecycle-heading">
        <div>
          <span className="section-kicker">VALIDATE</span>
          <h2>Upload the adapted Target rerun</h2>
          <p>Run the candidate prompt in the customer evaluator, then export one Target result file using the same schema.</p>
        </div>
        <Badge appearance="tint" color={result?.readyForRolloutReview ? "success" : result ? "warning" : "informative"}>
          {result?.readyForRolloutReview ? "Ready for rollout review" : result ? "Changes required" : "Rerun required"}
        </Badge>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept=".json,.jsonl,.csv,.xlsx"
        hidden
        onChange={(event) => {
          requestId.current += 1;
          const selected = event.target.files?.[0] ?? null;
          setFile(selected);
          setError("");
          onResult(null);
          event.target.value = "";
        }}
      />
      <div className="validation-upload">
        <div>
          <strong>{file?.name ?? "Adapted Target evaluation result"}</strong>
          <span>{file ? `${(file.size / 1024).toFixed(1)} KiB` : "JSON, JSONL, CSV, or XLSX; same Target schema and case IDs"}</span>
        </div>
        <Button onClick={() => inputRef.current?.click()}>{file ? "Replace file" : "Choose rerun file"}</Button>
        <Button appearance="primary" disabled={!file || loading} onClick={() => void validate()}>
          {loading ? "Validating…" : "Compare rerun"}
        </Button>
      </div>
      {loading && <Spinner label="Joining the rerun and checking resolved, remaining, and new failures…" />}
      {error && <MessageBar intent="error"><MessageBarBody>{error}</MessageBarBody></MessageBar>}
      {result && (
        <>
          <div className="validation-counts">
            {[
              ["Resolved", result.resolvedCount, "success"],
              ["Remaining", result.remainingCount, "warning"],
              ["New failures", result.newFailureCount, "danger"],
              ["Preserved", result.preservedCount, "neutral"],
              ["Unclassified", result.unclassifiedCount, "neutral"],
              ["Missing / extra", result.missingCount + result.extraCount, "danger"],
            ].map(([label, value, tone]) => (
              <article className={`validation-count ${tone}`} key={label}>
                <span>{label}</span><strong>{value}</strong>
              </article>
            ))}
          </div>
          {result.issues.filter((issue) => issue.severity === "error").map((issue, index) => (
            <MessageBar intent="error" key={index}>
              <MessageBarBody>{issue.caseId ? `${issue.caseId}: ` : ""}{issue.message}</MessageBarBody>
            </MessageBar>
          ))}
          <EvidenceNotes
            title="Validation notes"
            notes={[
              ...result.warnings,
              ...result.issues.filter((issue) => issue.severity === "warning").map((issue) => issue.message),
            ]}
          />
          <div className={`validation-gate ${result.readyForRolloutReview ? "ready" : "blocked"}`}>
            <strong>{result.readyForRolloutReview ? "Ready for customer rollout review" : "Not ready for rollout review"}</strong>
            <span>{result.readyForRolloutReview
              ? "No remaining, new, missing, extra, or unclassified cases were found."
              : "Review the remaining/new failures or coverage issues before handoff."}</span>
          </div>
        </>
      )}
    </section>
  );
}
