import { useEffect, useMemo, useRef, useState } from "react";
import { Button, MessageBar, MessageBarBody, Spinner } from "@fluentui/react-components";
import type { ConfirmedMapping } from "./evaluationMapping";
import { optimizationEvidenceCounts } from "./promptEligibility";
import { EvidenceNotes } from "./EvidenceNotes";
import {
  createOptimizationForm,
  validateOptimizationPreview,
  type OptimizationAnalysis,
  type OptimizationPreview,
  type OptimizerDeployment,
} from "./promptOptimization";

type Props = {
  token: string;
  prompt: { file: File; sha256: string };
  evidence: ConfirmedMapping;
  analysis: OptimizationAnalysis;
  sourceModel: string;
  targetModel: string;
  optimizer: OptimizerDeployment | null;
  optimizing: boolean;
  onOptimize: (preview: OptimizationPreview) => void;
};

function byteSize(bytes: number): string {
  return bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(1)} KiB` : `${(bytes / (1024 * 1024)).toFixed(2)} MiB`;
}

export function PromptOptimizationRequest({
  token, prompt, evidence, analysis, sourceModel, targetModel, optimizer, optimizing, onOptimize,
}: Props) {
  const [prepared, setPrepared] = useState<{ key: string; preview: OptimizationPreview } | null>(null);
  const [consent, setConsent] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const generation = useRef(0);
  const contextKey = JSON.stringify([
    prompt.sha256, evidence.preview.evaluationSha256, sourceModel, targetModel,
    optimizer?.accountName, optimizer?.modelName, optimizer?.deploymentName,
  ]);
  const counts = optimizationEvidenceCounts(analysis.cases);
  const current = prepared?.key === contextKey ? prepared.preview : null;
  const payloadText = useMemo(
    () => current?.request ? JSON.stringify(current.request, null, 2) : "",
    [current],
  );

  useEffect(() => {
    const id = ++generation.current;
    const controller = new AbortController();
    setPrepared(null);
    setConsent(false);
    setError("");
    setLoading(false);
    if (!optimizer) return;
    setLoading(true);
    const timeout = window.setTimeout(() => controller.abort(), 60_000);
    const prepare = async () => {
      try {
        const response = await fetch("/api/prompt-optimization-preview", {
          method: "POST",
          headers: { Authorization: `Bearer ${token}` },
          body: createOptimizationForm(prompt.file, evidence, sourceModel, targetModel, optimizer),
          signal: controller.signal,
        });
        const payload = await response.json();
        if (!response.ok) throw new Error(payload.error ?? `Request preparation failed (${response.status}).`);
        const preview = payload as OptimizationPreview;
        validateOptimizationPreview(preview, analysis, optimizer);
        if (id === generation.current) setPrepared({ key: contextKey, preview });
      } catch (failure) {
        if (id === generation.current) {
          setError(failure instanceof DOMException && failure.name === "AbortError"
            ? "Local payload preparation timed out. No content was sent to the optimizer."
            : failure instanceof Error ? failure.message : String(failure));
        }
      } finally {
        window.clearTimeout(timeout);
        if (id === generation.current) setLoading(false);
      }
    };
    void prepare();
    return () => {
      generation.current += 1;
      controller.abort();
      window.clearTimeout(timeout);
    };
  }, [
    contextKey, revision, analysis, evidence, prompt.file, token,
    sourceModel, targetModel, optimizer?.accountName, optimizer?.modelName, optimizer?.deploymentName,
  ]);

  const canOptimize = Boolean(current?.withinLimit && current.request && consent && optimizer && !loading && !optimizing);
  return (
    <>
      <div className="prompt-optimization-heading">
        <div>
          <span className="section-kicker">PROMPT OPTIMIZATION</span>
          <h3>{counts.caseCount > 0 ? "Let PromptV2 propose changes from the evidence" : "Generate a prompt-improvement candidate"}</h3>
          <p>
            {counts.caseCount > 0
              ? `All ${counts.caseCount} observed problem cases are included, without filtering by inferred cause or fixability. `
              : "No classified problem cases were found. This requests general prompt improvement, not a verified regression repair. "}
            The optimizer proposes a candidate; only a new evaluation can establish whether it helps.
          </p>
        </div>
        <Button
          className="prompt-optimize-button"
          appearance="primary"
          icon={<span aria-hidden="true">✦</span>}
          disabled={!canOptimize}
          onClick={() => {
            if (canOptimize && current) onOptimize(current);
          }}
        >
          {optimizing ? "Optimizing…" : "Prompt optimize"}
        </Button>
      </div>

      <div className="optimization-evidence-counts" aria-label="Optimization evidence scope">
        <span><strong>{counts.regressionCount}</strong> regressions</span>
        <span><strong>{counts.residualFailureCount}</strong> residual failures</span>
        <span><strong>{counts.operationalCount}</strong> operational findings</span>
        <span><strong>0</strong> cases omitted</span>
      </div>
      {!optimizer && (
        <MessageBar intent="warning">
          <MessageBarBody>
            No GPT-5.2 optimizer deployment was found. Select an available optimizer before sending a request.
          </MessageBarBody>
        </MessageBar>
      )}
      {loading && <Spinner size="small" label="Preparing the complete request locally; no model call yet…" />}
      {error && (
        <MessageBar intent="error">
          <MessageBarBody>
            {error}{" "}
            <Button size="small" disabled={optimizing} onClick={() => setRevision((value) => value + 1)}>
              Retry payload preparation
            </Button>
          </MessageBarBody>
        </MessageBar>
      )}
      {current && (
        <>
          <p className="optimization-request-size">
            Full request: <strong>{byteSize(current.requestBytes)}</strong>
            {" / "}{byteSize(current.maxRequestBytes)} local limit.
            No cases or fields are silently truncated. Service token limits may differ.
          </p>
          <EvidenceNotes notes={current.warnings} title="Request notes" />
          {!current.withinLimit && (
            <MessageBar intent="error">
              <MessageBarBody>
                The complete request exceeds the local size limit. Nothing will be sent or silently sampled.
                Use a smaller explicitly chosen evidence set; partial coverage must not be presented as the full dataset.
              </MessageBarBody>
            </MessageBar>
          )}
          {current.request && (
            <details className="optimization-payload">
              <summary>Review the exact outbound payload ({current.caseCount} cases)</summary>
              <p>
                Includes the Source prompt and case content inside <code>requested_changes</code>.
                Case content is evidence, not instructions. Missing context and references are not invented.
              </p>
              <pre>{payloadText}</pre>
            </details>
          )}
          <label className="mapping-checkbox optimization-consent">
            <input
              type="checkbox"
              checked={consent}
              disabled={!current.withinLimit || optimizing || loading}
              onChange={(event) => setConsent(event.target.checked)}
            />
            <span>
              I authorize sending the Source prompt and the complete evaluation evidence shown above
              {" "}to {optimizer?.accountName} / {optimizer?.deploymentName} ({optimizer?.modelName}).
              This may include Questions, model outputs, evaluator feedback and available references/context.
              This permission is separate from schema-only AI mapping.
            </span>
          </label>
        </>
      )}
    </>
  );
}
