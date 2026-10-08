import { useEffect, useState } from "react";
import { Button, MessageBar, MessageBarBody } from "@fluentui/react-components";
import { optimizationEvidenceCounts, type ComparisonFinding } from "./promptEligibility";
import type { OptimizerDeployment } from "./promptOptimization";

type Props = {
  analysis: { cases: readonly ComparisonFinding[] };
  optimizer: OptimizerDeployment | null;
  optimizing: boolean;
  onOptimize: () => void;
};

export function PromptOptimizationRequest({ analysis, optimizer, optimizing, onOptimize }: Props) {
  const [consent, setConsent] = useState(false);
  const counts = optimizationEvidenceCounts(analysis.cases);

  useEffect(() => {
    setConsent(false);
  }, [optimizer?.accountName, optimizer?.modelName, optimizer?.deploymentName]);

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
          disabled={!consent || !optimizer || optimizing}
          onClick={onOptimize}
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
      <label className="mapping-checkbox optimization-consent">
        <input
          type="checkbox"
          checked={consent}
          disabled={!optimizer || optimizing}
          onChange={(event) => setConsent(event.target.checked)}
        />
        <span>
          I authorize sending the Source prompt and complete evaluation evidence to{" "}
          {optimizer
            ? `${optimizer.accountName} / ${optimizer.deploymentName} (${optimizer.modelName})`
            : "the selected PromptV2 optimizer"}.
          This may include Questions, model outputs, evaluator feedback and available references/context.
          This permission is separate from schema-only AI mapping.
        </span>
      </label>
    </>
  );
}
