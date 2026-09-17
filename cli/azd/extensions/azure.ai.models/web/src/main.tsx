import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Badge,
  Button,
  FluentProvider,
  Input,
  MessageBar,
  MessageBarBody,
  Spinner,
  webLightTheme,
} from "@fluentui/react-components";
import {
  ArrowClockwise20Regular,
  CalendarClock20Regular,
  Cube20Regular,
  Search20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import {
  daysUntil,
  formatDate,
  retirementState,
  statusLabel,
  type ModelDeployment,
} from "./retirement";
import { parseEvidence, type EvidenceSummary } from "./evidence";
import { buildPromptDiff } from "./promptDiff";
import "./styles.css";

type ModelList = {
  subscriptionId: string;
  generatedAt: string;
  accounts: ModelAccount[];
  models: ModelDeployment[];
  warnings?: string[];
};

type ModelResourceList = {
  subscriptionId: string;
  generatedAt: string;
  resources: ModelAccount[];
};

type ResourceScan = {
  resource: ModelAccount;
  status: "pending" | "complete" | "warning" | "error";
  message?: string;
};

type ModelAccount = {
  name: string;
  kind: string;
  resourceGroup: string;
  location: string;
  resourceId: string;
};

type ReplacementRecommendation = {
  sourceModel: string;
  suggestedModel: string;
  suggestedFormat: string;
  source: "default";
};

type ModelCapability = {
  name: string;
  value: string;
};

type TargetAssessment = {
  targetModel: string;
  targetVersion?: string;
  targetFormat?: string;
  publisher?: string;
  lifecycleStatus?: string;
  retirementDate?: string;
  capabilities: ModelCapability[];
  warnings?: string[];
};

type DeploymentSKUOption = {
  name: string;
  availableCapacity?: number;
  quotaName?: string;
  quotaCurrent?: number;
  quotaLimit?: number;
};

type DeploymentOptions = {
  targetModel: string;
  targetVersion?: string;
  region: string;
  skus: DeploymentSKUOption[];
  warnings?: string[];
};

type EvidenceArtifact = {
  name: string;
  size: number;
  sha256: string;
  file: File;
  summary?: EvidenceSummary;
};

type EvidenceKind = "prompt" | "baseline" | "dataset" | "source" | "target";
type EvidenceMode = "combined" | "bundle";

type EvaluatorSummary = {
  name: string;
  sourcePassCount: number;
  targetPassCount: number;
  sourcePassRate: number;
  targetPassRate: number;
  sourceAverageScore?: number;
  targetAverageScore?: number;
};

type RegressionPattern = {
  code: string;
  label: string;
  count: number;
  prevalence: number;
  caseIds: string[];
  promptFixable: "candidate" | "no" | "unknown";
};

type RegressionCase = {
  caseId: string;
  question?: string;
  outcome: "regression" | "operational_regression" | "pre_existing_failure";
  sourceStatus: string;
  targetStatus: string;
  sourceOutput?: string;
  targetOutput?: string;
  evaluatorRationale?: string;
  failureKind: string;
  failureDetail?: string;
  expectedReasoning?: string;
  supportingEvidence?: string[];
  failureSource?: string;
  promptFixable: "candidate" | "no" | "unknown";
  confidence: "high" | "medium" | "low";
  latencyDeltaPercent?: number;
  tokenDeltaPercent?: number;
};

type EvaluationAnalysis = {
  fileName: string;
  format: string;
  sheetName?: string;
  sourceModel?: string;
  targetModel?: string;
  sourceRunId?: string;
  targetRunId?: string;
  evaluationId?: string;
  promptSha256: string;
  evaluationSha256: string;
  caseCount: number;
  stable: number;
  regressions: number;
  operationalRegressions: number;
  improvements: number;
  preExistingFailures: number;
  detectedFields: string[];
  evaluators: EvaluatorSummary[];
  patterns: RegressionPattern[];
  cases: RegressionCase[];
  warnings?: string[];
};

type PromptOptimizerComment = {
  kind: "explanation";
  location?: {
    before: { line: number; column: number };
    after: { line: number; column: number };
  };
  reason: string;
};

type PromptOptimizationResult = {
  sourcePrompt: string;
  optimizedPrompt: string;
  requestedChanges: string;
  comments: PromptOptimizerComment[];
  verificationCaseIds: string[];
  optimizerModel: string;
  optimizerDeployment: string;
  targetSpecific: boolean;
  promptSha256: string;
  evaluationSha256: string;
};

type TelemetryPreset = "1h" | "24h" | "7d" | "custom" | "evaluation";

type TelemetryWindow = {
  start: string;
  end: string;
};

type DeploymentMetricSummary = {
  deploymentName: string;
  requests?: number;
  errorRequests?: number;
  processedPromptTokens?: number;
  generatedTokens?: number;
  averageTtftMs?: number;
  averageTbtMs?: number;
  averageTtltMs?: number;
  series: MetricSeries[];
  warnings?: string[];
};

type MetricSeries = {
  name: string;
  unit: string;
  points: MetricPoint[];
};

type MetricPoint = {
  timestamp: string;
  value: number;
};

type DeploymentMetricsComparison = {
  startTime: string;
  endTime: string;
  source: DeploymentMetricSummary;
  target: DeploymentMetricSummary;
};

function sessionToken(): string {
  const current = new URL(window.location.href);
  const tokenFromURL = current.searchParams.get("token");
  if (tokenFromURL) {
    window.sessionStorage.setItem("migration-session-token", tokenFromURL);
    current.searchParams.delete("token");
    window.history.replaceState({}, "", current.pathname + current.search + current.hash);
    return tokenFromURL;
  }
  return window.sessionStorage.getItem("migration-session-token") ?? "";
}

const token = sessionToken();

function StatusBadge({ model }: { model: ModelDeployment }) {
  const state = retirementState(model);
  const appearance = state === "healthy" || state === "unknown" ? "tint" : "filled";
  const color =
    state === "retired" || state === "critical"
      ? "danger"
      : state === "upcoming"
        ? "warning"
        : state === "healthy"
          ? "success"
          : "informative";
  return (
    <Badge appearance={appearance} color={color} size="large">
      {statusLabel(model)}
    </Badge>
  );
}

function resourceTypeLabel(kind: string): string {
  if (kind.toLowerCase() === "openai") {
    return "Azure OpenAI resource";
  }
  if (kind.toLowerCase() === "aiservices") {
    return "Foundry resource";
  }
  return "Azure AI resource";
}

function skuLabel(name: string): string {
  const labels: Record<string, string> = {
    Standard: "Regional Standard",
    GlobalStandard: "Global Standard",
    DataZoneStandard: "Data Zone Standard",
    ProvisionedManaged: "Regional Provisioned",
    GlobalProvisionedManaged: "Global Provisioned",
    DataZoneProvisionedManaged: "Data Zone Provisioned",
  };
  return labels[name] ?? name;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function deploymentID(model: ModelDeployment): string {
  return model.resourceId || `${model.accountName}/${model.deploymentName}`;
}

function presetTelemetryWindow(preset: Exclude<TelemetryPreset, "custom" | "evaluation">): TelemetryWindow {
  const end = new Date();
  const durationHours = preset === "1h" ? 1 : preset === "24h" ? 24 : 24 * 7;
  return {
    start: new Date(end.getTime() - durationHours * 60 * 60 * 1000).toISOString(),
    end: end.toISOString(),
  };
}

function localDateTimeValue(value: string): string {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function accountResourceID(resourceID: string): string {
  const deploymentSegment = resourceID.toLowerCase().indexOf("/deployments/");
  return deploymentSegment >= 0 ? resourceID.slice(0, deploymentSegment) : resourceID;
}

function downloadText(fileName: string, content: string) {
  const url = URL.createObjectURL(new Blob([content], { type: "text/plain;charset=utf-8" }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = fileName;
  anchor.click();
  URL.revokeObjectURL(url);
}

function formatMetric(value: number | undefined, suffix = ""): string {
  return value === undefined
    ? "No data"
    : `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value)}${suffix}`;
}

function metricSeries(
  summary: DeploymentMetricSummary,
  name: string,
): MetricPoint[] {
  return summary.series?.find((series) => series.name === name)?.points ?? [];
}

function ComparisonLineChart({
  title,
  unit,
  source,
  target,
}: {
  title: string;
  unit: string;
  source: MetricPoint[];
  target: MetricPoint[];
}) {
  const allPoints = [...source, ...target];
  if (allPoints.length === 0) {
    return (
      <article className="metric-chart empty">
        <div>
          <strong>{title}</strong>
          <span>{unit}</span>
        </div>
        <p>No Monitor data in this window.</p>
      </article>
    );
  }

  const width = 620;
  const height = 190;
  const padding = { top: 18, right: 18, bottom: 28, left: 44 };
  const timestamps = allPoints.map((point) => Date.parse(point.timestamp));
  const minimumTime = Math.min(...timestamps);
  const maximumTime = Math.max(...timestamps);
  const maximumValue = Math.max(...allPoints.map((point) => point.value), 1);
  const x = (timestamp: string) => {
    const value = Date.parse(timestamp);
    const range = maximumTime - minimumTime;
    return padding.left +
      (range === 0 ? 0.5 : (value - minimumTime) / range) *
        (width - padding.left - padding.right);
  };
  const y = (value: number) =>
    padding.top +
    (1 - value / maximumValue) * (height - padding.top - padding.bottom);
  const line = (points: MetricPoint[]) =>
    points.map((point) => `${x(point.timestamp)},${y(point.value)}`).join(" ");
  const timeLabel = (timestamp: number) =>
    new Intl.DateTimeFormat(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    }).format(timestamp);

  return (
    <article className="metric-chart">
      <div>
        <strong>{title}</strong>
        <span>{unit}</span>
      </div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={`${title}: Source and Target Azure Monitor time series`}
      >
        {[0, 0.5, 1].map((ratio) => (
          <g key={ratio}>
            <line
              x1={padding.left}
              x2={width - padding.right}
              y1={padding.top + ratio * (height - padding.top - padding.bottom)}
              y2={padding.top + ratio * (height - padding.top - padding.bottom)}
            />
            <text
              x={padding.left - 8}
              y={padding.top + ratio * (height - padding.top - padding.bottom) + 4}
              textAnchor="end"
            >
              {formatMetric(maximumValue * (1 - ratio))}
            </text>
          </g>
        ))}
        {source.length > 0 && (
          <polyline className="source-line" points={line(source)} />
        )}
        {source.length === 1 && (
          <circle
            className="source-point"
            cx={x(source[0].timestamp)}
            cy={y(source[0].value)}
            r="4"
          />
        )}
        {target.length > 0 && (
          <polyline className="target-line" points={line(target)} />
        )}
        {target.length === 1 && (
          <circle
            className="target-point"
            cx={x(target[0].timestamp)}
            cy={y(target[0].value)}
            r="4"
          />
        )}
        <text x={padding.left} y={height - 7}>
          {timeLabel(minimumTime)}
        </text>
        <text x={width - padding.right} y={height - 7} textAnchor="end">
          {timeLabel(maximumTime)}
        </text>
      </svg>
      <footer>
        <span className="source-key">Source</span>
        <span className="target-key">Target</span>
      </footer>
    </article>
  );
}

async function sha256File(file: File): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
  return Array.from(new Uint8Array(digest))
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

async function sha256Text(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest))
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

function modelNamesMatch(actual: string | undefined, expected: string): boolean {
  if (!actual || !expected) {
    return true;
  }
  const normalizedActual = actual.toLowerCase();
  const normalizedExpected = expected.toLowerCase();
  return (
    normalizedActual === normalizedExpected ||
    normalizedActual.startsWith(`${normalizedExpected}-`) ||
    normalizedExpected.startsWith(`${normalizedActual}-`)
  );
}

const workflowSteps = ["Discover", "Assess", "Adapt", "Validate", "Roll out", "Retire"];

type WorkflowStep = "discover" | "assess" | "adapt";

function WorkflowNavigation({ activeStep }: { activeStep: WorkflowStep }) {
  const activeIndex = { discover: 0, assess: 1, adapt: 2 }[activeStep];
  return (
    <nav className="workflow-nav" aria-label="Migration workflow">
      <ol>
        {workflowSteps.map((step, index) => (
          <li
            key={step}
            className={
              index < activeIndex ? "complete" : index === activeIndex ? "active" : "upcoming"
            }
            aria-current={index === activeIndex ? "step" : undefined}
          >
            <span>{index + 1}</span>
            <strong>{step}</strong>
          </li>
        ))}
      </ol>
    </nav>
  );
}

function App() {
  const [data, setData] = useState<ModelList | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [attentionOnly, setAttentionOnly] = useState(false);
  const [selectedModel, setSelectedModel] = useState<ModelDeployment | null>(null);
  const [activeStep, setActiveStep] = useState<WorkflowStep>("discover");
  const [recommendation, setRecommendation] = useState<ReplacementRecommendation | null>(null);
  const [recommendationError, setRecommendationError] = useState("");
  const [recommendationLoading, setRecommendationLoading] = useState(false);
  const [assessment, setAssessment] = useState<TargetAssessment | null>(null);
  const [assessmentError, setAssessmentError] = useState("");
  const [assessmentLoading, setAssessmentLoading] = useState(false);
  const [deploymentChoice, setDeploymentChoice] = useState<"existing" | "new">("new");
  const [selectedTargetDeploymentId, setSelectedTargetDeploymentId] = useState("");
  const [resourceScans, setResourceScans] = useState<ResourceScan[]>([]);
  const [createRegion, setCreateRegion] = useState("");
  const [selectedSKU, setSelectedSKU] = useState("");
  const [deploymentOptions, setDeploymentOptions] = useState<DeploymentOptions | null>(null);
  const [deploymentOptionsError, setDeploymentOptionsError] = useState("");
  const [deploymentOptionsLoading, setDeploymentOptionsLoading] = useState(false);
  const [promptArtifact, setPromptArtifact] = useState<EvidenceArtifact | null>(null);
  const [evidenceMode, setEvidenceMode] = useState<EvidenceMode>("combined");
  const [baselineArtifact, setBaselineArtifact] = useState<EvidenceArtifact | null>(null);
  const [datasetArtifact, setDatasetArtifact] = useState<EvidenceArtifact | null>(null);
  const [sourceArtifact, setSourceArtifact] = useState<EvidenceArtifact | null>(null);
  const [targetArtifact, setTargetArtifact] = useState<EvidenceArtifact | null>(null);
  const [evidenceError, setEvidenceError] = useState<Record<EvidenceKind, string>>({
    prompt: "",
    baseline: "",
    dataset: "",
    source: "",
    target: "",
  });
  const [evaluationAnalysis, setEvaluationAnalysis] =
    useState<EvaluationAnalysis | null>(null);
  const [evaluationAnalysisError, setEvaluationAnalysisError] = useState("");
  const [evaluationAnalysisLoading, setEvaluationAnalysisLoading] = useState(false);
  const [promptOptimization, setPromptOptimization] =
    useState<PromptOptimizationResult | null>(null);
  const [promptOptimizationError, setPromptOptimizationError] = useState("");
  const [promptOptimizationLoading, setPromptOptimizationLoading] = useState(false);
  const [adaptSourceDeploymentId, setAdaptSourceDeploymentId] = useState("");
  const [adaptTargetDeploymentId, setAdaptTargetDeploymentId] = useState("");
  const [telemetryPreset, setTelemetryPreset] = useState<TelemetryPreset>("24h");
  const [telemetryWindow, setTelemetryWindow] = useState<TelemetryWindow>(() =>
    presetTelemetryWindow("24h"),
  );
  const [deploymentMetrics, setDeploymentMetrics] =
    useState<DeploymentMetricsComparison | null>(null);
  const [deploymentMetricsError, setDeploymentMetricsError] = useState("");
  const [deploymentMetricsLoading, setDeploymentMetricsLoading] = useState(false);
  const promptInputRef = useRef<HTMLInputElement>(null);
  const baselineInputRef = useRef<HTMLInputElement>(null);
  const datasetInputRef = useRef<HTMLInputElement>(null);
  const sourceInputRef = useRef<HTMLInputElement>(null);
  const targetInputRef = useRef<HTMLInputElement>(null);
  const assessmentRequestId = useRef(0);
  const deploymentOptionsRequestId = useRef(0);
  const inventoryRequestId = useRef(0);
  const evaluationAnalysisRequestId = useRef(0);
  const promptOptimizationRequestId = useRef(0);
  const evidenceReadRequestIds = useRef<Record<EvidenceKind, number>>({
    prompt: 0,
    baseline: 0,
    dataset: 0,
    source: 0,
    target: 0,
  });

  const scanResource = async (resource: ModelAccount, requestId: number) => {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 25_000);
    try {
      const response = await fetch("/api/resource-models", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(resource),
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      if (requestId !== inventoryRequestId.current) {
        return;
      }
      const result = payload as ModelList;
      setData((current) =>
        current
          ? {
              ...current,
              models: [...current.models, ...(result.models ?? [])],
            }
          : current,
      );
      setResourceScans((current) =>
        current.map((scan) =>
          scan.resource.resourceId === resource.resourceId
            ? {
                ...scan,
                status: result.warnings?.length ? "warning" : "complete",
                message: result.warnings?.join(" "),
              }
            : scan,
        ),
      );
    } catch (requestError) {
      if (requestId !== inventoryRequestId.current) {
        return;
      }
      const message =
        requestError instanceof DOMException && requestError.name === "AbortError"
          ? "This resource did not respond before the timeout."
          : requestError instanceof Error
            ? requestError.message
            : String(requestError);
      setResourceScans((current) =>
        current.map((scan) =>
          scan.resource.resourceId === resource.resourceId
            ? { ...scan, status: "error", message }
            : scan,
        ),
      );
    } finally {
      window.clearTimeout(timeout);
    }
  };

  const scanResources = async (resources: ModelAccount[], requestId: number) => {
    let nextIndex = 0;
    const worker = async () => {
      while (requestId === inventoryRequestId.current) {
        const index = nextIndex;
        nextIndex += 1;
        if (index >= resources.length) {
          return;
        }
        await scanResource(resources[index], requestId);
      }
    };
    await Promise.all(
      Array.from({ length: Math.min(6, resources.length) }, () => worker()),
    );
  };

  const loadModels = async () => {
    const requestId = ++inventoryRequestId.current;
    setLoading(true);
    setError("");
    setData(null);
    setResourceScans([]);
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 30_000);
    try {
      const response = await fetch("/api/resources", {
        headers: { Authorization: `Bearer ${token}` },
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      if (requestId !== inventoryRequestId.current) {
        return;
      }
      const result = payload as ModelResourceList;
      const resources = result.resources ?? [];
      setData({
        subscriptionId: result.subscriptionId,
        generatedAt: result.generatedAt,
        accounts: resources,
        models: [],
      });
      setResourceScans(
        resources.map((resource) => ({ resource, status: "pending" })),
      );
      void scanResources(resources, requestId);
    } catch (requestError) {
      if (requestId === inventoryRequestId.current) {
        setError(
          requestError instanceof DOMException && requestError.name === "AbortError"
            ? "Azure AI resource discovery timed out. Retry the scan or choose another subscription."
            : requestError instanceof Error
              ? requestError.message
              : String(requestError),
        );
      }
    } finally {
      window.clearTimeout(timeout);
      if (requestId === inventoryRequestId.current) {
        setLoading(false);
      }
    }
  };

  useEffect(() => {
    void loadModels();
  }, []);

  useEffect(() => {
    setDeploymentMetrics(null);
    setDeploymentMetricsError("");
  }, [
    adaptSourceDeploymentId,
    adaptTargetDeploymentId,
    telemetryWindow.start,
    telemetryWindow.end,
  ]);

  const filteredModels = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return (data?.models ?? []).filter((model) => {
      const matchesQuery =
        !normalizedQuery ||
        [
          model.deploymentName,
          model.modelName,
          model.modelVersion,
          model.accountName,
          model.resourceGroup,
          model.location,
        ].some((value) => value.toLowerCase().includes(normalizedQuery));
      const state = retirementState(model);
      const needsAttention = state === "retired" || state === "critical" || state === "upcoming";
      return matchesQuery && (!attentionOnly || needsAttention);
    });
  }, [attentionOnly, data, query]);

  const summary = useMemo(() => {
    const models = data?.models ?? [];
    return {
      total: models.length,
      urgent: models.filter((model) => {
        const state = retirementState(model);
        return state === "retired" || state === "critical";
      }).length,
      upcoming: models.filter((model) => retirementState(model) === "upcoming").length,
    };
  }, [data]);

  const existingTargetDeployments = useMemo(() => {
    if (!recommendation) {
      return [];
    }
    return (data?.models ?? []).filter(
      (model) =>
        model.modelName.localeCompare(recommendation.suggestedModel, undefined, {
          sensitivity: "accent",
        }) === 0 &&
        model.modelFormat.localeCompare(recommendation.suggestedFormat, undefined, {
          sensitivity: "accent",
        }) === 0,
    );
  }, [data, recommendation]);

  const selectedTargetDeployment =
    existingTargetDeployments.find(
      (model) => deploymentID(model) === selectedTargetDeploymentId,
    ) ?? null;
  const adaptSourceDeployment =
    (data?.models ?? []).find((model) => deploymentID(model) === adaptSourceDeploymentId) ??
    null;
  const adaptTargetDeployment =
    existingTargetDeployments.find(
      (model) => deploymentID(model) === adaptTargetDeploymentId,
    ) ?? null;
  const promptOptimizerDeployment =
    (data?.models ?? []).find(
      (model) =>
        model.modelName.toLowerCase() === "gpt-5.2" &&
        model.accountName === adaptTargetDeployment?.accountName,
    ) ??
    (data?.models ?? []).find(
      (model) => model.modelName.toLowerCase() === "gpt-5.2",
    ) ??
    null;
  const incompleteResourceScans = resourceScans.filter(
    (scan) => scan.status !== "complete",
  );
  const regions = useMemo(
    () =>
      Array.from(
        new Set((data?.accounts ?? []).map((account) => account.location).filter(Boolean)),
      ).sort(),
    [data],
  );
  const selectedDeploymentSKU =
    deploymentOptions?.skus.find((sku) => sku.name === selectedSKU) ?? null;
  const bundleSummary: EvidenceSummary | undefined =
    datasetArtifact?.summary && sourceArtifact?.summary && targetArtifact?.summary
      ? {
          caseCount: datasetArtifact.summary.caseCount,
          caseIds: datasetArtifact.summary.caseIds,
          sourceModel: sourceArtifact.summary.runModel,
          targetModel: targetArtifact.summary.runModel,
          sourceRunId: sourceArtifact.summary.runId,
          targetRunId: targetArtifact.summary.runId,
          format: "foundry-run",
        }
      : undefined;
  const activeEvidenceSummary =
    evidenceMode === "combined" ? baselineArtifact?.summary : bundleSummary;
  const baselineSourceMatches =
    !activeEvidenceSummary?.sourceModel ||
    !adaptSourceDeployment ||
    modelNamesMatch(activeEvidenceSummary.sourceModel, adaptSourceDeployment.modelName);
  const expectedTargetModel =
    adaptTargetDeployment?.modelName ?? recommendation?.suggestedModel ?? "";
  const baselineTargetMatches =
    !activeEvidenceSummary?.targetModel ||
    !expectedTargetModel ||
    modelNamesMatch(activeEvidenceSummary.targetModel, expectedTargetModel);
  const promptHashMatches =
    !activeEvidenceSummary?.promptSha256 ||
    !promptArtifact ||
    activeEvidenceSummary.promptSha256 === promptArtifact.sha256;
  const telemetryWindowReady =
    telemetryWindow.start !== "" &&
    telemetryWindow.end !== "" &&
    Date.parse(telemetryWindow.start) < Date.parse(telemetryWindow.end);
  const evidenceReady =
    promptArtifact !== null &&
    (evidenceMode === "combined"
      ? baselineArtifact !== null
      : datasetArtifact !== null &&
        sourceArtifact !== null &&
        targetArtifact !== null) &&
    adaptSourceDeploymentId !== "" &&
    adaptTargetDeploymentId !== "" &&
    adaptTargetDeploymentId !== "planned-target" &&
    baselineSourceMatches &&
    baselineTargetMatches &&
    promptHashMatches;
  const selectedTargetLabel =
    deploymentChoice === "existing" && selectedTargetDeployment
      ? selectedTargetDeployment.deploymentName
      : `${recommendation?.suggestedModel ?? "gpt-5.4"} · ${createRegion || "Region pending"} · ${
          selectedDeploymentSKU ? skuLabel(selectedDeploymentSKU.name) : "SKU pending"
        }`;
  const promptDiff = useMemo(
    () =>
      promptOptimization
        ? buildPromptDiff(
            promptOptimization.sourcePrompt,
            promptOptimization.optimizedPrompt,
          )
        : [],
    [promptOptimization],
  );
  const promptFixableTargetFailureCount =
    evaluationAnalysis?.cases.filter(
      (item) =>
        (item.outcome === "regression" || item.outcome === "pre_existing_failure") &&
        item.promptFixable === "candidate",
    ).length ?? 0;

  const resetAdaptEvidence = () => {
    evaluationAnalysisRequestId.current += 1;
    promptOptimizationRequestId.current += 1;
    evidenceReadRequestIds.current.prompt += 1;
    evidenceReadRequestIds.current.baseline += 1;
    evidenceReadRequestIds.current.dataset += 1;
    evidenceReadRequestIds.current.source += 1;
    evidenceReadRequestIds.current.target += 1;
    setPromptArtifact(null);
    setBaselineArtifact(null);
    setDatasetArtifact(null);
    setSourceArtifact(null);
    setTargetArtifact(null);
    setEvidenceError({ prompt: "", baseline: "", dataset: "", source: "", target: "" });
    setEvaluationAnalysis(null);
    setEvaluationAnalysisError("");
    setEvaluationAnalysisLoading(false);
    setPromptOptimization(null);
    setPromptOptimizationError("");
    setPromptOptimizationLoading(false);
  };

  const openModel = (model: ModelDeployment) => {
    setSelectedModel(model);
    setActiveStep("discover");
    setRecommendation(null);
    setRecommendationError("");
    setAssessment(null);
    setAssessmentError("");
    setDeploymentChoice("new");
    setSelectedTargetDeploymentId("");
    setCreateRegion("");
    setSelectedSKU("");
    setDeploymentOptions(null);
    setDeploymentOptionsError("");
    resetAdaptEvidence();
    void requestRecommendation(model);
    window.scrollTo({ top: 0, behavior: "smooth" });
  };

  const closeModel = () => {
    setSelectedModel(null);
    setActiveStep("discover");
    setRecommendation(null);
    setRecommendationError("");
    setAssessment(null);
    setAssessmentError("");
    setDeploymentChoice("new");
    setSelectedTargetDeploymentId("");
    setCreateRegion("");
    setSelectedSKU("");
    setDeploymentOptions(null);
    setDeploymentOptionsError("");
    resetAdaptEvidence();
  };

  const requestRecommendation = async (model: ModelDeployment) => {
    setRecommendationLoading(true);
    setRecommendationError("");
    try {
      const response = await fetch("/api/recommendations", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          modelName: model.modelName,
          modelVersion: model.modelVersion,
          modelFormat: model.modelFormat,
        }),
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      setRecommendation(payload as ReplacementRecommendation);
    } catch (requestError) {
      setRecommendationError(
        requestError instanceof Error ? requestError.message : String(requestError),
      );
    } finally {
      setRecommendationLoading(false);
    }
  };

  const requestAssessment = async (existingDeployment: ModelDeployment) => {
    if (!recommendation) {
      return;
    }
    const requestId = ++assessmentRequestId.current;
    setAssessment(null);
    setAssessmentError("");
    setAssessmentLoading(true);
    try {
      const response = await fetch("/api/assessments", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          resourceGroup: existingDeployment.resourceGroup,
          accountName: existingDeployment.accountName,
          targetModel: recommendation.suggestedModel,
          targetVersion: existingDeployment.modelVersion,
          targetFormat: recommendation.suggestedFormat,
        }),
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      if (requestId === assessmentRequestId.current) {
        setAssessment(payload as TargetAssessment);
      }
    } catch (requestError) {
      if (requestId === assessmentRequestId.current) {
        setAssessmentError(
          requestError instanceof Error ? requestError.message : String(requestError),
        );
      }
    } finally {
      if (requestId === assessmentRequestId.current) {
        setAssessmentLoading(false);
      }
    }
  };

  const requestDeploymentOptions = async (region: string) => {
    if (!selectedModel || !recommendation || !region) {
      return;
    }
    const requestId = ++deploymentOptionsRequestId.current;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 25_000);
    setCreateRegion(region);
    setSelectedSKU("");
    setDeploymentOptions(null);
    setDeploymentOptionsError("");
    setDeploymentOptionsLoading(true);
    resetAdaptEvidence();
    try {
      const response = await fetch("/api/deployment-options", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          resourceGroup: selectedModel.resourceGroup,
          accountName: selectedModel.accountName,
          region,
          targetModel: recommendation.suggestedModel,
          targetFormat: recommendation.suggestedFormat,
        }),
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      if (requestId === deploymentOptionsRequestId.current) {
        const options = payload as DeploymentOptions;
        setDeploymentOptions(options);
        setSelectedSKU(options.skus?.[0]?.name ?? "");
      }
    } catch (requestError) {
      if (requestId === deploymentOptionsRequestId.current) {
        setDeploymentOptionsError(
          requestError instanceof DOMException && requestError.name === "AbortError"
            ? `Checking deployment options in ${region} timed out.`
            : requestError instanceof Error
              ? requestError.message
              : String(requestError),
        );
      }
    } finally {
      window.clearTimeout(timeout);
      if (requestId === deploymentOptionsRequestId.current) {
        setDeploymentOptionsLoading(false);
      }
    }
  };

  const startAssessment = () => {
    const existingDeployment = existingTargetDeployments[0] ?? null;
    setDeploymentChoice(existingDeployment ? "existing" : "new");
    setSelectedTargetDeploymentId(
      existingDeployment
        ? existingDeployment.resourceId ||
            `${existingDeployment.accountName}/${existingDeployment.deploymentName}`
        : "",
    );
    setActiveStep("assess");
    window.scrollTo({ top: 0, behavior: "smooth" });
    if (existingDeployment) {
      void requestAssessment(existingDeployment);
    } else {
      setAssessment(null);
      setAssessmentError("");
      setAssessmentLoading(false);
      const defaultRegion = selectedModel?.location || regions[0] || "";
      if (defaultRegion) {
        void requestDeploymentOptions(defaultRegion);
      }
    }
  };

  const chooseExistingDeployment = (deployment: ModelDeployment) => {
    deploymentOptionsRequestId.current += 1;
    setDeploymentChoice("existing");
    setSelectedTargetDeploymentId(
      deployment.resourceId || `${deployment.accountName}/${deployment.deploymentName}`,
    );
    setDeploymentOptions(null);
    setDeploymentOptionsError("");
    setDeploymentOptionsLoading(false);
    resetAdaptEvidence();
    void requestAssessment(deployment);
  };

  const chooseNewDeployment = () => {
    assessmentRequestId.current += 1;
    setDeploymentChoice("new");
    setSelectedTargetDeploymentId("");
    setAssessment(null);
    setAssessmentError("");
    setAssessmentLoading(false);
    resetAdaptEvidence();
    const defaultRegion = createRegion || selectedModel?.location || regions[0] || "";
    if (defaultRegion) {
      void requestDeploymentOptions(defaultRegion);
    }
  };

  const readEvidenceFile = async (kind: EvidenceKind, file: File | null) => {
    if (!file) {
      return;
    }
    const requestId = ++evidenceReadRequestIds.current[kind];
    evaluationAnalysisRequestId.current += 1;
    promptOptimizationRequestId.current += 1;
    switch (kind) {
      case "prompt":
        setPromptArtifact(null);
        break;
      case "baseline":
        setBaselineArtifact(null);
        break;
      case "dataset":
        setDatasetArtifact(null);
        break;
      case "source":
        setSourceArtifact(null);
        break;
      case "target":
        setTargetArtifact(null);
        break;
    }
    setEvidenceError((current) => ({ ...current, [kind]: "" }));
    setEvaluationAnalysis(null);
    setEvaluationAnalysisError("");
    setPromptOptimization(null);
    setPromptOptimizationError("");
    setEvaluationAnalysisLoading(false);
    setPromptOptimizationLoading(false);
    try {
      if (file.size === 0) {
        throw new Error("The file is empty.");
      }
      const extension = file.name.split(".").at(-1)?.toLowerCase();
      let summary: EvidenceSummary | undefined;
      if (kind === "prompt") {
        const content = await file.text();
        if (!content.trim()) {
          throw new Error("The file is empty.");
        }
      } else if (extension !== "xlsx") {
        summary = parseEvidence(await file.text());
        if (kind === "baseline" && summary.format !== "combined") {
          throw new Error(
            "This is a Foundry dataset or run export. Switch to Run bundle and upload all three artifacts.",
          );
        }
        if (kind === "dataset" && summary.format !== "foundry-dataset") {
          throw new Error("Choose the Foundry generated dataset JSONL file.");
        }
        if (
          (kind === "source" || kind === "target") &&
          summary.format !== "foundry-run"
        ) {
          throw new Error("Choose a Foundry eval.run.output_item results JSONL file.");
        }
      }
      const artifact: EvidenceArtifact = {
        name: file.name,
        size: file.size,
        sha256: await sha256File(file),
        file,
        summary,
      };

      if (requestId !== evidenceReadRequestIds.current[kind]) {
        return;
      }
      switch (kind) {
        case "prompt":
          setPromptArtifact(artifact);
          break;
        case "baseline":
          setBaselineArtifact(artifact);
          break;
        case "dataset":
          setDatasetArtifact(artifact);
          break;
        case "source":
          setSourceArtifact(artifact);
          break;
        case "target":
          setTargetArtifact(artifact);
          break;
      }
      if (kind !== "prompt") {
        if (artifact.summary?.startedAt && artifact.summary.completedAt) {
          setTelemetryPreset("evaluation");
          setTelemetryWindow({
            start: artifact.summary.startedAt,
            end: artifact.summary.completedAt,
          });
        }
      }
    } catch (fileError) {
      if (requestId !== evidenceReadRequestIds.current[kind]) {
        return;
      }
      switch (kind) {
        case "prompt":
          setPromptArtifact(null);
          break;
        case "baseline":
          setBaselineArtifact(null);
          break;
        case "dataset":
          setDatasetArtifact(null);
          break;
        case "source":
          setSourceArtifact(null);
          break;
        case "target":
          setTargetArtifact(null);
          break;
      }
      setEvidenceError((current) => ({
        ...current,
        [kind]: fileError instanceof Error ? fileError.message : String(fileError),
      }));
    }
  };

  const changeEvidenceMode = (mode: EvidenceMode) => {
    if (mode === evidenceMode) {
      return;
    }
    evaluationAnalysisRequestId.current += 1;
    promptOptimizationRequestId.current += 1;
    setEvidenceMode(mode);
    setEvaluationAnalysis(null);
    setEvaluationAnalysisError("");
    setPromptOptimization(null);
    setPromptOptimizationError("");
    setEvaluationAnalysisLoading(false);
    setPromptOptimizationLoading(false);
  };

  const appendEvaluationEvidence = (form: FormData) => {
    if (evidenceMode === "combined" && baselineArtifact) {
      form.append("evaluation", baselineArtifact.file, baselineArtifact.name);
      return;
    }
    if (datasetArtifact && sourceArtifact && targetArtifact) {
      form.append("dataset", datasetArtifact.file, datasetArtifact.name);
      form.append("sourceEvaluation", sourceArtifact.file, sourceArtifact.name);
      form.append("targetEvaluation", targetArtifact.file, targetArtifact.name);
    }
  };

  const currentEvaluationSHA256 = async () => {
    if (evidenceMode === "combined") {
      return baselineArtifact?.sha256 ?? "";
    }
    if (!datasetArtifact || !sourceArtifact || !targetArtifact) {
      return "";
    }
    return sha256Text(
      `${datasetArtifact.sha256}:${sourceArtifact.sha256}:${targetArtifact.sha256}`,
    );
  };

  const analyzeEvaluation = async () => {
    if (!promptArtifact || !evidenceReady) {
      return;
    }
    const requestId = ++evaluationAnalysisRequestId.current;
    promptOptimizationRequestId.current += 1;
    const expectedPromptSHA256 = promptArtifact.sha256;
    const expectedEvaluationSHA256 = await currentEvaluationSHA256();
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 60_000);
    setEvaluationAnalysis(null);
    setEvaluationAnalysisError("");
    setPromptOptimization(null);
    setPromptOptimizationError("");
    setEvaluationAnalysisLoading(true);
    try {
      const form = new FormData();
      form.append("prompt", promptArtifact.file, promptArtifact.name);
      appendEvaluationEvidence(form);
      if (adaptSourceDeployment) {
        form.append("sourceModelName", adaptSourceDeployment.modelName);
      }
      if (adaptTargetDeployment) {
        form.append("targetModelName", adaptTargetDeployment.modelName);
      }
      const response = await fetch("/api/evaluation-analysis", {
        method: "POST",
        headers: {
          Authorization: "Bearer " + token,
        },
        body: form,
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      const result = payload as EvaluationAnalysis;
      if (requestId !== evaluationAnalysisRequestId.current) {
        return;
      }
      if (
        result.promptSha256 !== expectedPromptSHA256 ||
        result.evaluationSha256 !== expectedEvaluationSHA256
      ) {
        throw new Error("The evaluation response does not match the currently loaded files.");
      }
      setEvaluationAnalysis(result);
    } catch (requestError) {
      if (requestId === evaluationAnalysisRequestId.current) {
        setEvaluationAnalysisError(
          requestError instanceof DOMException && requestError.name === "AbortError"
            ? "Evaluation analysis timed out."
            : requestError instanceof Error
              ? requestError.message
              : String(requestError),
        );
      }
    } finally {
      window.clearTimeout(timeout);
      if (requestId === evaluationAnalysisRequestId.current) {
        setEvaluationAnalysisLoading(false);
      }
    }
  };

  const optimizePrompt = async () => {
    if (
      !promptArtifact ||
      !evidenceReady ||
      !evaluationAnalysis ||
      !adaptSourceDeployment ||
      !adaptTargetDeployment ||
      !promptOptimizerDeployment
    ) {
      return;
    }
    const requestId = ++promptOptimizationRequestId.current;
    const expectedPromptSHA256 = promptArtifact.sha256;
    const expectedEvaluationSHA256 = await currentEvaluationSHA256();
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 130_000);
    setPromptOptimization(null);
    setPromptOptimizationError("");
    setPromptOptimizationLoading(true);
    try {
      const form = new FormData();
      form.append("prompt", promptArtifact.file, promptArtifact.name);
      appendEvaluationEvidence(form);
      form.append("sourceModelName", adaptSourceDeployment.modelName);
      form.append("targetModelName", adaptTargetDeployment.modelName);
      form.append("optimizerAccountName", promptOptimizerDeployment.accountName);
      form.append("optimizerModelName", promptOptimizerDeployment.modelName);
      form.append(
        "optimizerDeploymentName",
        promptOptimizerDeployment.deploymentName,
      );
      const response = await fetch("/api/prompt-optimization", {
        method: "POST",
        headers: {
          Authorization: "Bearer " + token,
        },
        body: form,
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      const result = payload as PromptOptimizationResult;
      if (requestId !== promptOptimizationRequestId.current) {
        return;
      }
      if (
        result.promptSha256 !== expectedPromptSHA256 ||
        result.evaluationSha256 !== expectedEvaluationSHA256
      ) {
        throw new Error("The PromptV2 result does not match the currently loaded files.");
      }
      setPromptOptimization(result);
    } catch (requestError) {
      if (requestId === promptOptimizationRequestId.current) {
        setPromptOptimizationError(
          requestError instanceof DOMException && requestError.name === "AbortError"
            ? "PromptV2 optimization timed out."
            : requestError instanceof Error
              ? requestError.message
              : String(requestError),
        );
      }
    } finally {
      window.clearTimeout(timeout);
      if (requestId === promptOptimizationRequestId.current) {
        setPromptOptimizationLoading(false);
      }
    }
  };

  const queryDeploymentMetrics = async () => {
    if (!adaptSourceDeployment || !adaptTargetDeployment || !telemetryWindowReady) {
      return;
    }
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 35_000);
    setDeploymentMetrics(null);
    setDeploymentMetricsError("");
    setDeploymentMetricsLoading(true);
    try {
      const response = await fetch("/api/deployment-metrics", {
        method: "POST",
        headers: {
          Authorization: "Bearer " + token,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          source: {
            resourceId: accountResourceID(adaptSourceDeployment.resourceId),
            location: adaptSourceDeployment.location,
            deploymentName: adaptSourceDeployment.deploymentName,
          },
          target: {
            resourceId: accountResourceID(adaptTargetDeployment.resourceId),
            location: adaptTargetDeployment.location,
            deploymentName: adaptTargetDeployment.deploymentName,
          },
          startTime: telemetryWindow.start,
          endTime: telemetryWindow.end,
        }),
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      setDeploymentMetrics(payload as DeploymentMetricsComparison);
    } catch (requestError) {
      setDeploymentMetricsError(
        requestError instanceof DOMException && requestError.name === "AbortError"
          ? "Azure Monitor metrics query timed out."
          : requestError instanceof Error
            ? requestError.message
            : String(requestError),
      );
    } finally {
      window.clearTimeout(timeout);
      setDeploymentMetricsLoading(false);
    }
  };

  const startAdapt = () => {
    if (!selectedModel) {
      return;
    }
    setAdaptSourceDeploymentId(deploymentID(selectedModel));
    setAdaptTargetDeploymentId(
      selectedTargetDeployment ? deploymentID(selectedTargetDeployment) : "planned-target",
    );
    setTelemetryPreset("24h");
    setTelemetryWindow(presetTelemetryWindow("24h"));
    setActiveStep("adapt");
    window.scrollTo({ top: 0, behavior: "smooth" });
  };

  const newTargetReady =
    selectedDeploymentSKU?.availableCapacity !== undefined &&
    selectedDeploymentSKU.availableCapacity > 0 &&
    selectedDeploymentSKU.quotaCurrent !== undefined &&
    selectedDeploymentSKU.quotaLimit !== undefined &&
    selectedDeploymentSKU.quotaCurrent < selectedDeploymentSKU.quotaLimit;
  const canStartAdapt =
    deploymentChoice === "existing"
      ? selectedTargetDeployment !== null && assessment !== null
      : newTargetReady;

  if (selectedModel && activeStep === "adapt") {
    const artifacts: Array<{
      kind: EvidenceKind;
      title: string;
      description: string;
      accept: string;
      artifact: EvidenceArtifact | null;
      inputRef: React.RefObject<HTMLInputElement>;
    }> = [
      {
        kind: "prompt",
        title: "Source prompt",
        description: "Exact system/developer prompt used for both unchanged runs.",
        accept: ".md,.txt,.yaml,.yml,.json",
        artifact: promptArtifact,
        inputRef: promptInputRef,
      },
      ...(evidenceMode === "combined"
        ? [
            {
              kind: "baseline" as const,
              title: "Combined evaluation",
              description:
                "One paired XLSX/JSON/JSONL containing cases, Source/Target outputs, evaluator results, and metrics.",
              accept: ".xlsx,.json,.jsonl",
              artifact: baselineArtifact,
              inputRef: baselineInputRef,
            },
          ]
        : [
            {
              kind: "dataset" as const,
              title: "Dataset",
              description:
                "Foundry generated JSONL with id, query, description, and candidate_response.",
              accept: ".json,.jsonl",
              artifact: datasetArtifact,
              inputRef: datasetInputRef,
            },
            {
              kind: "source" as const,
              title: "Source results",
              description:
                "Foundry eval.run.output_item JSONL produced by the Source model.",
              accept: ".json,.jsonl",
              artifact: sourceArtifact,
              inputRef: sourceInputRef,
            },
            {
              kind: "target" as const,
              title: "Target results",
              description:
                "Foundry eval.run.output_item JSONL produced by the Target model.",
              accept: ".json,.jsonl",
              artifact: targetArtifact,
              inputRef: targetInputRef,
            },
          ]),
    ];

    return (
      <FluentProvider theme={webLightTheme}>
        <main className="page-shell adapt-page">
          <div className="ambient ambient-one" />
          <div className="ambient ambient-two" />

          <Button
            appearance="subtle"
            className="back-button"
            onClick={() => setActiveStep("assess")}
          >
            ← Back to assessment
          </Button>

          <WorkflowNavigation activeStep="adapt" />

          <header className="hero adapt-hero">
            <div className="eyebrow">
              <span className="eyebrow-mark" />
              MODEL MIGRATION · ADAPT
            </div>
            <div className="hero-row">
              <div>
                <h1>Prepare migration evidence</h1>
                <p>
                  Use the Source and Target fixed in Discover and Assess, then load their
                  unchanged comparison results before validation.
                </p>
              </div>
              <Button appearance="primary" size="large" disabled>
                Continue to Validate
              </Button>
            </div>
          </header>

          <section className="comparison-setup">
            <div className="evidence-heading">
              <div>
                <span className="section-kicker">COMPARISON</span>
                <h2>Confirmed deployment pair</h2>
                <p>
                  This migration keeps the Source and Target selected in the previous
                  steps. Uploaded results are checked against this fixed pair.
                </p>
              </div>
            </div>
            <div className="deployment-pair">
              <article>
                <span>Source deployment</span>
                <strong>{adaptSourceDeployment?.deploymentName ?? "Unavailable"}</strong>
                <small>
                  {adaptSourceDeployment
                    ? `${adaptSourceDeployment.modelName} · ${adaptSourceDeployment.location} · ${adaptSourceDeployment.accountName}`
                    : "Return to Discover and select a Source deployment."}
                </small>
              </article>
              <span className="route-arrow">→</span>
              <article>
                <span>Target deployment</span>
                <strong>
                  {adaptTargetDeployment?.deploymentName ?? selectedTargetLabel}
                </strong>
                <small>
                  {adaptTargetDeployment
                    ? `${adaptTargetDeployment.modelName} · ${adaptTargetDeployment.location} · ${adaptTargetDeployment.accountName}`
                    : "Planned Target from Assess; create and discover it before querying Monitor."}
                </small>
              </article>
            </div>
            <div className="telemetry-window">
              <div>
                <span className="section-kicker">AZURE MONITOR WINDOW</span>
                <strong>Scope operational metrics to the evaluation period</strong>
                <small>
                  Requests, latency, errors, and token metrics must use the same time range
                  for both deployments.
                </small>
              </div>
              <label>
                <span>Time window</span>
                <select
                  value={telemetryPreset}
                  onChange={(event) => {
                    const preset = event.target.value as TelemetryPreset;
                    setTelemetryPreset(preset);
                    if (preset === "1h" || preset === "24h" || preset === "7d") {
                      setTelemetryWindow(presetTelemetryWindow(preset));
                    }
                  }}
                >
                  <option value="1h">Last 1 hour</option>
                  <option value="24h">Last 24 hours</option>
                  <option value="7d">Last 7 days</option>
                  {telemetryPreset === "evaluation" && (
                    <option value="evaluation">Evaluation run window</option>
                  )}
                  <option value="custom">Custom range</option>
                </select>
              </label>
              <label>
                <span>Start</span>
                <input
                  type="datetime-local"
                  value={localDateTimeValue(telemetryWindow.start)}
                  onChange={(event) => {
                    setTelemetryPreset("custom");
                    setTelemetryWindow((current) => ({
                      ...current,
                      start: event.target.value
                        ? new Date(event.target.value).toISOString()
                        : "",
                    }));
                  }}
                />
              </label>
              <label>
                <span>End</span>
                <input
                  type="datetime-local"
                  value={localDateTimeValue(telemetryWindow.end)}
                  onChange={(event) => {
                    setTelemetryPreset("custom");
                    setTelemetryWindow((current) => ({
                      ...current,
                      end: event.target.value
                        ? new Date(event.target.value).toISOString()
                        : "",
                    }));
                  }}
                />
              </label>
              <output>
                {telemetryWindowReady
                  ? `${telemetryWindow.start} → ${telemetryWindow.end}`
                  : "Enter a valid start and end time."}
              </output>
              <Button
                appearance="primary"
                disabled={
                  deploymentMetricsLoading ||
                  !telemetryWindowReady ||
                  !adaptSourceDeployment ||
                  !adaptTargetDeployment
                }
                onClick={() => void queryDeploymentMetrics()}
              >
                {deploymentMetricsLoading ? "Querying Monitor…" : "Query Azure Monitor"}
              </Button>
            </div>
            {deploymentMetricsError && (
              <MessageBar intent="error">
                <MessageBarBody>{deploymentMetricsError}</MessageBarBody>
              </MessageBar>
            )}
            {deploymentMetrics && (
              <div className="monitor-results">
                <div className="monitor-results-heading">
                  <div>
                    <span className="section-kicker">LIVE AZURE MONITOR DATA</span>
                    <strong>
                      {deploymentMetrics.startTime} → {deploymentMetrics.endTime}
                    </strong>
                  </div>
                  <Badge appearance="tint" color="success">
                    Query complete
                  </Badge>
                </div>
                <div className="monitor-comparison">
                  {[
                    { label: "Source", value: deploymentMetrics.source },
                    { label: "Target", value: deploymentMetrics.target },
                  ].map(({ label, value }) => (
                    <article key={label}>
                      <div>
                        <span>{label}</span>
                        <strong>{value.deploymentName}</strong>
                      </div>
                      <dl>
                        <div>
                          <dt>Requests</dt>
                          <dd>{formatMetric(value.requests)}</dd>
                        </div>
                        <div>
                          <dt>Error requests</dt>
                          <dd>{formatMetric(value.errorRequests)}</dd>
                        </div>
                        <div>
                          <dt>Input tokens</dt>
                          <dd>{formatMetric(value.processedPromptTokens)}</dd>
                        </div>
                        <div>
                          <dt>Output tokens</dt>
                          <dd>{formatMetric(value.generatedTokens)}</dd>
                        </div>
                        <div>
                          <dt>Average TTFT</dt>
                          <dd>{formatMetric(value.averageTtftMs, " ms")}</dd>
                        </div>
                        <div>
                          <dt>Average TBT</dt>
                          <dd>{formatMetric(value.averageTbtMs, " ms")}</dd>
                        </div>
                        <div>
                          <dt>Average TTLT</dt>
                          <dd>{formatMetric(value.averageTtltMs, " ms")}</dd>
                        </div>
                      </dl>
                      {value.warnings?.map((warning) => (
                        <small key={warning}>{warning}</small>
                      ))}
                    </article>
                  ))}
                </div>
                <div className="monitor-chart-heading">
                  <div>
                    <span className="section-kicker">TIME-SERIES COMPARISON</span>
                    <strong>Source and Target by Monitor time bucket</strong>
                  </div>
                  <small>
                    These lines use Azure Monitor averages and totals. Request-level P50/P95
                    requires diagnostic logs or evaluation traces and is not inferred here.
                  </small>
                </div>
                <div className="metric-chart-grid">
                  {[
                    { name: "ttft", title: "Time to first token", unit: "Average ms" },
                    { name: "tbt", title: "Time between tokens", unit: "Average ms" },
                    { name: "ttlt", title: "Time to last token", unit: "Average ms" },
                    { name: "inputTokens", title: "Input tokens", unit: "Total per bucket" },
                    { name: "outputTokens", title: "Output tokens", unit: "Total per bucket" },
                    { name: "requestRate", title: "Request rate", unit: "Requests / min" },
                  ].map((chart) => (
                    <ComparisonLineChart
                      key={chart.name}
                      title={chart.title}
                      unit={chart.unit}
                      source={metricSeries(deploymentMetrics.source, chart.name)}
                      target={metricSeries(deploymentMetrics.target, chart.name)}
                    />
                  ))}
                </div>
              </div>
            )}
            {adaptTargetDeploymentId === "planned-target" && (
              <MessageBar intent="warning">
                <MessageBarBody>
                  This Target is still a deployment plan. Create and evaluate it before
                  loading baseline results.
                </MessageBarBody>
              </MessageBar>
            )}
          </section>

          <section className="evidence-section">
            <div className="evidence-heading">
              <div>
                <span className="section-kicker">EVALUATION RESULTS</span>
                <h2>Load the unchanged comparison</h2>
                <p>
                  Use one paired comparison export, or assemble a Foundry bundle from the
                  dataset and the two independently downloaded model runs.
                </p>
              </div>
              <Badge appearance="tint" color={evidenceReady ? "success" : "warning"}>
                {evidenceReady
                  ? "Ready to analyze"
                  : evidenceMode === "combined"
                    ? "2 artifacts required"
                    : "4 artifacts required"}
              </Badge>
            </div>

            <div className="evidence-mode-switch" aria-label="Evaluation evidence format">
              <button
                type="button"
                className={evidenceMode === "combined" ? "active" : ""}
                onClick={() => changeEvidenceMode("combined")}
              >
                <strong>Combined export</strong>
                <span>Meera or canonical paired evidence</span>
              </button>
              <button
                type="button"
                className={evidenceMode === "bundle" ? "active" : ""}
                onClick={() => changeEvidenceMode("bundle")}
              >
                <strong>Run bundle</strong>
                <span>Foundry dataset + Source + Target</span>
              </button>
            </div>

            <div className="evidence-grid">
              {artifacts.map(({ kind, title, description, accept, artifact, inputRef }, index) => (
                <article
                  key={kind}
                  className={`evidence-card ${artifact ? "ready" : ""}`}
                >
                  <input
                    ref={inputRef}
                    type="file"
                    accept={accept}
                    hidden
                    onChange={(event) =>
                      void readEvidenceFile(kind, event.target.files?.[0] ?? null)
                    }
                  />
                  <div className="evidence-card-topline">
                    <span className="evidence-number">0{index + 1}</span>
                    <Badge
                      appearance="tint"
                      color={artifact ? "success" : "informative"}
                    >
                      {artifact ? "Ready" : "Required"}
                    </Badge>
                  </div>
                  <h3>{artifact?.name ?? title}</h3>
                  <p>
                    {artifact
                      ? `${formatBytes(artifact.size)} · SHA-256 ${artifact.sha256.slice(0, 10)}…`
                      : description}
                  </p>
                  {artifact?.summary && (
                    <strong>{artifact.summary.caseCount} cases detected</strong>
                  )}
                  <Button appearance="secondary" onClick={() => inputRef.current?.click()}>
                    {artifact ? "Replace file" : "Choose file"}
                  </Button>
                  {evidenceError[kind] && <em>{evidenceError[kind]}</em>}
                </article>
              ))}
            </div>

            {!promptHashMatches && (
              <MessageBar intent="error">
                <MessageBarBody>
                  Source prompt hash does not match suite.prompt_sha256 in the evaluation
                  result.
                </MessageBarBody>
              </MessageBar>
            )}
            {!telemetryWindowReady && (
              <MessageBar intent="error">
                <MessageBarBody>
                  Select a valid telemetry window with an end time after its start time.
                </MessageBarBody>
              </MessageBar>
            )}

            <div className="analysis-action">
              <div>
                <strong>Analyze the customer-provided evaluation</strong>
                <span>
                  Files are joined by case ID and checked for matching queries, references,
                  run lineage, evaluator results, and Source/Target model identity.
                </span>
              </div>
              <Button
                appearance="primary"
                size="large"
                disabled={!evidenceReady || evaluationAnalysisLoading}
                onClick={() => void analyzeEvaluation()}
              >
                {evaluationAnalysisLoading ? "Analyzing…" : "Analyze evidence"}
              </Button>
            </div>
            {evaluationAnalysisLoading && (
              <div className="analysis-loading">
                <Spinner label="Parsing evaluation evidence and comparing Source with Target…" />
              </div>
            )}
            {evaluationAnalysisError && (
              <MessageBar intent="error">
                <MessageBarBody>{evaluationAnalysisError}</MessageBarBody>
              </MessageBar>
            )}

            {activeEvidenceSummary && (
              <div className="evaluation-result">
                <div className="evaluation-result-heading">
                  <div>
                    <span className="section-kicker">RESULT SUMMARY</span>
                    <h3>
                      {evidenceMode === "combined"
                        ? baselineArtifact?.name
                        : "Foundry evaluation bundle"}
                    </h3>
                  </div>
                  <Badge
                    appearance="tint"
                    color={
                      baselineSourceMatches && baselineTargetMatches ? "success" : "danger"
                    }
                  >
                    {baselineSourceMatches && baselineTargetMatches
                      ? "Deployments matched"
                      : "Deployment mismatch"}
                  </Badge>
                </div>
                <div className="evaluation-run-grid">
                  <div>
                    <span>Source run</span>
                    <strong>
                      {activeEvidenceSummary.sourceRunId || "Run ID not reported"}
                    </strong>
                    <small>
                      {activeEvidenceSummary.sourceModel || "Model not reported"} ·{" "}
                      {adaptSourceDeployment?.deploymentName || "No deployment selected"}
                    </small>
                  </div>
                  <div>
                    <span>Target run</span>
                    <strong>
                      {activeEvidenceSummary.targetRunId || "Run ID not reported"}
                    </strong>
                    <small>
                      {activeEvidenceSummary.targetModel || "Model not reported"} ·{" "}
                      {adaptTargetDeployment?.deploymentName ||
                        (adaptTargetDeploymentId === "planned-target"
                          ? "Planned deployment"
                          : "No deployment selected")}
                    </small>
                  </div>
                </div>
                <div className="evaluation-metrics">
                  <div>
                    <span>Cases</span>
                    <strong>{activeEvidenceSummary.caseCount}</strong>
                  </div>
                  <div>
                    <span>Stable</span>
                    <strong>{activeEvidenceSummary.stable ?? "—"}</strong>
                  </div>
                  <div>
                    <span>Regressions</span>
                    <strong>{activeEvidenceSummary.regressions ?? "—"}</strong>
                  </div>
                  <div>
                    <span>Improvements</span>
                    <strong>{activeEvidenceSummary.improvements ?? "—"}</strong>
                  </div>
                </div>
                {!baselineSourceMatches && (
                  <MessageBar intent="error">
                    <MessageBarBody>
                      Result Source model {activeEvidenceSummary.sourceModel} does not
                      match {adaptSourceDeployment?.modelName}.
                    </MessageBarBody>
                  </MessageBar>
                )}
                {!baselineTargetMatches && (
                  <MessageBar intent="error">
                    <MessageBarBody>
                      Result Target model {activeEvidenceSummary.targetModel} does not
                      match {expectedTargetModel}.
                    </MessageBarBody>
                  </MessageBar>
                )}
              </div>
            )}

            {evaluationAnalysis && (
              <div className="regression-analysis">
                <div className="evaluation-result-heading">
                  <div>
                    <span className="section-kicker">REGRESSION ANALYSIS</span>
                    <h3>
                      {evaluationAnalysis.sheetName
                        ? `${evaluationAnalysis.fileName} · ${evaluationAnalysis.sheetName}`
                        : evaluationAnalysis.fileName}
                    </h3>
                  </div>
                  <Badge appearance="tint" color="success">
                    Customer evaluation analyzed
                  </Badge>
                </div>

                <div className="analysis-summary-grid">
                  {[
                    { label: "Cases", value: evaluationAnalysis.caseCount, tone: "neutral" },
                    { label: "Stable", value: evaluationAnalysis.stable, tone: "stable" },
                    {
                      label: "Quality regressions",
                      value: evaluationAnalysis.regressions,
                      tone: "regression",
                    },
                    {
                      label: "Operational",
                      value: evaluationAnalysis.operationalRegressions,
                      tone: "operational",
                    },
                    {
                      label: "Improvements",
                      value: evaluationAnalysis.improvements,
                      tone: "improvement",
                    },
                    {
                      label: "Pre-existing failures",
                      value: evaluationAnalysis.preExistingFailures,
                      tone: "neutral",
                    },
                  ].map((metric) => (
                    <article key={metric.label} className={`analysis-summary ${metric.tone}`}>
                      <span>{metric.label}</span>
                      <strong>{metric.value}</strong>
                    </article>
                  ))}
                </div>

                <div className="analysis-grid">
                  <section className="analysis-panel">
                    <div className="analysis-panel-heading">
                      <div>
                        <span className="section-kicker">EVALUATOR DELTAS</span>
                        <strong>Source and Target pass rates</strong>
                      </div>
                    </div>
                    <div className="evaluator-list">
                      {evaluationAnalysis.evaluators.map((evaluator) => (
                        <article key={evaluator.name}>
                          <div className="evaluator-heading">
                            <strong>{evaluator.name}</strong>
                            <span>
                              {Math.round(
                                (evaluator.targetPassRate - evaluator.sourcePassRate) * 100,
                              )}
                              {" pp"}
                            </span>
                          </div>
                          <div className="pass-rate-row">
                            <span>Source</span>
                            <div>
                              <i style={{ width: `${evaluator.sourcePassRate * 100}%` }} />
                            </div>
                            <strong>{Math.round(evaluator.sourcePassRate * 100)}%</strong>
                          </div>
                          <div className="pass-rate-row target">
                            <span>Target</span>
                            <div>
                              <i style={{ width: `${evaluator.targetPassRate * 100}%` }} />
                            </div>
                            <strong>{Math.round(evaluator.targetPassRate * 100)}%</strong>
                          </div>
                        </article>
                      ))}
                    </div>
                  </section>

                  <section className="analysis-panel">
                    <div className="analysis-panel-heading">
                      <div>
                        <span className="section-kicker">FAILURE PATTERNS</span>
                        <strong>Recurring Target failure patterns</strong>
                      </div>
                    </div>
                    <div className="pattern-list">
                      {evaluationAnalysis.patterns.map((pattern) => (
                        <article key={pattern.code}>
                          <div>
                            <strong>{pattern.label}</strong>
                            <span>{pattern.caseIds.join(", ")}</span>
                          </div>
                          <div>
                            <strong>{pattern.count}</strong>
                            <span>{Math.round(pattern.prevalence * 100)}% of cases</span>
                          </div>
                          <Badge
                            appearance="tint"
                            color={
                              pattern.promptFixable === "candidate"
                                ? "success"
                                : pattern.promptFixable === "no"
                                  ? "informative"
                                  : "warning"
                            }
                          >
                            {pattern.promptFixable === "candidate"
                              ? "Prompt-fixable"
                              : pattern.promptFixable === "no"
                                ? "Outside prompt"
                                : "Needs review"}
                          </Badge>
                        </article>
                      ))}
                    </div>
                  </section>
                </div>

                <section className="regression-cases">
                  <div className="analysis-panel-heading">
                    <div>
                      <span className="section-kicker">CASE EVIDENCE</span>
                      <strong>Target failures reported by the customer evaluation</strong>
                    </div>
                    <span>{evaluationAnalysis.cases.length} cases</span>
                  </div>
                  <div className="regression-case-list">
                    {evaluationAnalysis.cases.map((item) => (
                      <details key={item.caseId}>
                        <summary>
                          <span>
                            <strong>{item.caseId}</strong>
                            <small>{item.question || item.failureDetail}</small>
                          </span>
                          <Badge
                            appearance="tint"
                            color={
                              item.outcome === "operational_regression"
                                ? "warning"
                                : "danger"
                            }
                          >
                            {item.failureKind.replaceAll("_", " ")}
                          </Badge>
                          <span className="case-transition">
                            {item.sourceStatus} → {item.targetStatus}
                          </span>
                        </summary>
                        <div className="case-evidence-grid">
                          <article>
                            <span>Source output</span>
                            <p>{item.sourceOutput || "Not supplied"}</p>
                          </article>
                          <article>
                            <span>Target output</span>
                            <p>{item.targetOutput || "Not supplied"}</p>
                          </article>
                        </div>
                        <div className="case-diagnosis">
                          <strong>{item.failureDetail || item.evaluatorRationale}</strong>
                          {item.evaluatorRationale &&
                            item.evaluatorRationale !== item.failureDetail && (
                              <p>Evaluator: {item.evaluatorRationale}</p>
                            )}
                          {item.expectedReasoning && (
                            <p>Expected reasoning: {item.expectedReasoning}</p>
                          )}
                          <small>
                            Confidence {item.confidence} ·{" "}
                            {item.promptFixable === "candidate"
                              ? "Prompt-fixable candidate"
                              : item.promptFixable === "no"
                                ? "Not a Prompt fix"
                                : "Needs more evidence"}
                          </small>
                        </div>
                      </details>
                    ))}
                  </div>
                </section>

                <section className="prompt-optimization">
                  <div className="prompt-optimization-heading">
                    <div>
                      <span className="section-kicker">PROMPT OPTIMIZATION</span>
                      <h3>Generate an evidence-backed PromptV2 candidate</h3>
                      <p>
                        PromptV2 receives the Source prompt plus server-derived pattern
                        counts and directives from {promptFixableTargetFailureCount}{" "}
                        prompt-fixable Target failure
                        {promptFixableTargetFailureCount === 1 ? "" : "s"}, including
                        migration regressions and residual failures. Customer free text,
                        operational failures, and retrieval failures stay outside the
                        instruction channel. The rewrite runs on{" "}
                        {promptOptimizerDeployment
                          ? `${promptOptimizerDeployment.modelName} · ${promptOptimizerDeployment.deploymentName}`
                          : "a compatible GPT-5.2 deployment"}.
                      </p>
                    </div>
                    <Button
                          className="prompt-optimize-button"
                          appearance="primary"
                          icon={<span aria-hidden="true">✦</span>}
                          disabled={
                            promptOptimizationLoading ||
                            promptFixableTargetFailureCount === 0 ||
                            promptOptimizerDeployment === null
                          }
                          onClick={() => void optimizePrompt()}
                    >
                          {promptOptimizationLoading
                            ? "Optimizing…"
                            : "Prompt optimize"}
                    </Button>
                  </div>

                  {promptOptimizationLoading && (
                    <div className="analysis-loading">
                      <Spinner label="Sending Target failure evidence to PromptV2…" />
                    </div>
                  )}
                  {promptFixableTargetFailureCount === 0 && (
                    <MessageBar intent="warning">
                      <MessageBarBody>
                        No prompt-fixable Target failures were found. PromptV2 will not
                        rewrite the prompt for operational or retrieval failures.
                      </MessageBarBody>
                    </MessageBar>
                  )}
                  {!promptOptimizerDeployment && (
                    <MessageBar intent="warning">
                      <MessageBarBody>
                        No GPT-5.2 deployment was found in the selected subscription.
                        PromptV2 needs a real GPT-5.2 deployment to run the optimizer.
                      </MessageBarBody>
                    </MessageBar>
                  )}
                  {promptOptimizationError && (
                    <MessageBar intent="error">
                      <MessageBarBody>{promptOptimizationError}</MessageBarBody>
                    </MessageBar>
                  )}

                  {promptOptimization && (
                    <div className="prompt-candidate">
                      <div className="prompt-candidate-heading">
                        <div>
                          <span className="section-kicker">ADAPTED PROMPT</span>
                          <h3>PromptV2 migration candidate</h3>
                        </div>
                        <Badge appearance="tint" color="success">
                          Ready for validation
                        </Badge>
                      </div>

                      <div className="prompt-candidate-meta">
                        <div>
                          <span>Optimizer model</span>
                          <strong>{promptOptimization.optimizerModel}</strong>
                          <small>{promptOptimization.optimizerDeployment}</small>
                        </div>
                        <div>
                          <span>Evidence scope</span>
                          <strong>
                            {promptOptimization.verificationCaseIds.length} cases
                          </strong>
                          <small>
                            {promptOptimization.verificationCaseIds.join(", ")}
                          </small>
                        </div>
                        <div>
                          <span>Optimization mode</span>
                          <strong>Target-failure-steered</strong>
                          <small>
                            {promptOptimization.targetSpecific
                              ? "Target-specific"
                              : "Evidence-driven, not target-specific"}
                          </small>
                        </div>
                      </div>

                      <div className="prompt-candidate-next-step">
                        <strong>Next step</strong>
                        <span>
                          Run this candidate against the same evaluation before
                          promotion.
                        </span>
                      </div>

                      <div className="prompt-candidate-toolbar">
                        <strong>Source → optimized prompt diff</strong>
                        <Button
                          appearance="secondary"
                          onClick={() =>
                            downloadText(
                              `${promptOptimization.optimizerDeployment}-optimized-prompt.txt`,
                              promptOptimization.optimizedPrompt,
                            )
                          }
                        >
                          Download optimized prompt
                        </Button>
                      </div>
                      <pre className="prompt-diff" aria-label="Prompt diff">
                        {promptDiff.map((line, index) => (
                          <span key={`${line.kind}-${index}`} className={line.kind}>
                            <i>
                              {line.kind === "added"
                                ? "+"
                                : line.kind === "removed"
                                  ? "−"
                                  : " "}
                            </i>
                            <code>{line.value || " "}</code>
                          </span>
                        ))}
                      </pre>

                      {promptOptimization.comments.length > 0 && (
                        <div className="prompt-comments">
                          <strong>PromptV2 change rationale</strong>
                          {promptOptimization.comments.map((comment, index) => (
                            <article key={`${comment.reason}-${index}`}>
                              <span>{String(index + 1).padStart(2, "0")}</span>
                              <p>{comment.reason}</p>
                            </article>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </section>

                {evaluationAnalysis.warnings?.map((warning) => (
                  <MessageBar key={warning} intent="warning">
                    <MessageBarBody>{warning}</MessageBarBody>
                  </MessageBar>
                ))}
              </div>
            )}
          </section>

          <footer>
            Adapt uses customer evaluation evidence to generate a PromptV2 candidate.
            Validation still requires rerunning the same evaluation in the customer runner,
            CI pipeline, or Foundry Evaluation.
          </footer>
        </main>
      </FluentProvider>
    );
  }

  if (selectedModel && activeStep === "assess") {
    return (
      <FluentProvider theme={webLightTheme}>
        <main className="page-shell assess-page">
          <div className="ambient ambient-one" />
          <div className="ambient ambient-two" />

          <Button
            appearance="subtle"
            className="back-button"
            onClick={() => setActiveStep("discover")}
          >
            ← Back to model comparison
          </Button>

          <WorkflowNavigation activeStep="assess" />

          <header className="hero assess-hero">
            <div className="eyebrow">
              <span className="eyebrow-mark" />
              MODEL MIGRATION · ASSESS
            </div>
            <div className="hero-row">
              <div>
                <h1>Assess {recommendation?.suggestedModel ?? "target model"}</h1>
                <p>
                  Reuse an existing target deployment or start a new deployment.
                </p>
              </div>
              <Button
                appearance="primary"
                size="large"
                disabled={!canStartAdapt}
                onClick={startAdapt}
              >
                Continue to Adapt
              </Button>
            </div>
          </header>

          <section className="model-route" aria-label="Migration route">
            <div>
              <span>Source</span>
              <strong>{selectedModel.modelName}</strong>
              <small>{selectedModel.modelVersion || "Default version"}</small>
            </div>
            <span className="route-arrow">→</span>
            <div className="target">
              <span>Target</span>
              <strong>{recommendation?.suggestedModel ?? "GPT-5.4"}</strong>
              <small>
                {deploymentChoice === "existing" && selectedTargetDeployment
                  ? selectedTargetDeployment.deploymentName
                  : "Create new deployment"}
              </small>
            </div>
          </section>

          <section className="deployment-choice">
            <div className="deployment-choice-heading">
              <div>
                <span className="section-kicker">DEPLOYMENT PATH</span>
                <h2>Choose where to migrate</h2>
              </div>
              <div className="deployment-choice-actions">
                <Badge appearance="tint" color="informative">
                  {existingTargetDeployments.length} existing found
                </Badge>
                <Button
                  appearance={deploymentChoice === "new" ? "primary" : "secondary"}
                  onClick={
                    deploymentChoice === "new" && existingTargetDeployments.length > 0
                      ? () => chooseExistingDeployment(existingTargetDeployments[0])
                      : chooseNewDeployment
                  }
                >
                  {deploymentChoice === "new" && existingTargetDeployments.length > 0
                    ? "Use existing deployment"
                    : "Create new deployment"}
                </Button>
              </div>
            </div>

            {deploymentChoice === "existing" && (
              <div className="deployment-options">
                {existingTargetDeployments.map((deployment) => {
                  const deploymentId =
                    deployment.resourceId ||
                    `${deployment.accountName}/${deployment.deploymentName}`;
                  const selected = selectedTargetDeploymentId === deploymentId;
                  return (
                    <button
                      key={deploymentId}
                      type="button"
                      className={`deployment-option ${selected ? "selected" : ""}`}
                      onClick={() => chooseExistingDeployment(deployment)}
                    >
                      <span className="option-marker" aria-hidden="true" />
                      <span>
                        <small>Use existing deployment</small>
                        <strong>{deployment.deploymentName}</strong>
                        <em>
                          {deployment.accountName} · {deployment.location} ·{" "}
                          {deployment.modelVersion || "Default version"}
                        </em>
                      </span>
                    </button>
                  );
                })}
              </div>
            )}

            {deploymentChoice === "new" && (
              <div className="create-deployment-panel">
                <div className="create-fields">
                  <label>
                    <span>Region</span>
                    <select
                      value={createRegion}
                      onChange={(event) => void requestDeploymentOptions(event.target.value)}
                    >
                      {regions.map((region) => (
                        <option key={region} value={region}>
                          {region}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    <span>SKU</span>
                    <select
                      value={selectedSKU}
                      disabled={deploymentOptionsLoading || !deploymentOptions?.skus.length}
                      onChange={(event) => {
                        setSelectedSKU(event.target.value);
                        resetAdaptEvidence();
                      }}
                    >
                      {!selectedSKU && <option value="">Select a SKU</option>}
                      {deploymentOptions?.skus.map((sku) => (
                        <option key={sku.name} value={sku.name}>
                          {skuLabel(sku.name)}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>

                {deploymentOptionsLoading && (
                  <Spinner size="small" label={`Checking ${createRegion}…`} />
                )}
                {deploymentOptionsError && (
                  <MessageBar intent="error">
                    <MessageBarBody>{deploymentOptionsError}</MessageBarBody>
                  </MessageBar>
                )}
                {deploymentOptions?.warnings?.map((warning) => (
                  <MessageBar key={warning} intent="warning">
                    <MessageBarBody>{warning}</MessageBarBody>
                  </MessageBar>
                ))}
                {deploymentOptions && !deploymentOptionsLoading && deploymentOptions.skus.length === 0 && (
                  <div className="deployment-empty">
                    <strong>No deployable SKU was reported</strong>
                    <span>Try another region.</span>
                  </div>
                )}
                {selectedDeploymentSKU && (
                  <div className="deployment-signal-grid">
                    <div>
                      <span>Availability</span>
                      <strong>
                        {selectedDeploymentSKU.availableCapacity === undefined
                          ? "Not reported"
                          : selectedDeploymentSKU.availableCapacity > 0
                            ? "Available"
                            : "No capacity reported"}
                      </strong>
                      <small>
                        {selectedDeploymentSKU.availableCapacity === undefined
                          ? "No regional capacity signal returned"
                          : `${selectedDeploymentSKU.availableCapacity} capacity available`}
                      </small>
                    </div>
                    <div>
                      <span>Quota</span>
                      <strong>
                        {selectedDeploymentSKU.quotaCurrent !== undefined &&
                        selectedDeploymentSKU.quotaLimit !== undefined
                          ? `${selectedDeploymentSKU.quotaCurrent} / ${selectedDeploymentSKU.quotaLimit} used`
                          : "Not reported"}
                      </strong>
                      <small>
                        {selectedDeploymentSKU.quotaCurrent !== undefined &&
                        selectedDeploymentSKU.quotaLimit !== undefined
                          ? `${Math.max(
                              selectedDeploymentSKU.quotaLimit -
                                selectedDeploymentSKU.quotaCurrent,
                              0,
                            )} remaining`
                          : "No matching quota metric returned"}
                      </small>
                    </div>
                  </div>
                )}
                <p className="deployment-signal-note">
                  Availability and quota are current control-plane signals, not a deployment
                  guarantee.
                </p>
              </div>
            )}
          </section>

          {deploymentChoice === "existing" && assessmentError && (
            <MessageBar intent="error">
              <MessageBarBody>{assessmentError}</MessageBarBody>
            </MessageBar>
          )}

          {deploymentChoice === "existing" && (assessmentLoading ? (
            <section className="assessment-loading">
              <Spinner size="large" label="Loading target model details…" />
            </section>
          ) : assessment ? (
            <>
              {assessment.warnings?.map((warning) => (
                <MessageBar key={warning} intent="warning">
                  <MessageBarBody>{warning}</MessageBarBody>
                </MessageBar>
              ))}

              <section className="assessment-grid">
                <article className="assessment-card">
                  <span className="section-kicker">MODEL CATALOG</span>
                  <h2>Target metadata</h2>
                  <dl>
                    <div>
                      <dt>Model</dt>
                      <dd>{assessment.targetModel}</dd>
                    </div>
                    <div>
                      <dt>Version</dt>
                      <dd>
                        {selectedTargetDeployment?.modelVersion ||
                          assessment.targetVersion ||
                          "Unavailable"}
                      </dd>
                    </div>
                    <div>
                      <dt>Format</dt>
                      <dd>
                        {selectedTargetDeployment?.modelFormat ||
                          assessment.targetFormat ||
                          "Unavailable"}
                      </dd>
                    </div>
                    <div>
                      <dt>Publisher</dt>
                      <dd>{assessment.publisher || "Not reported"}</dd>
                    </div>
                    <div>
                      <dt>Lifecycle</dt>
                      <dd>
                        {selectedTargetDeployment?.lifecycleStatus ||
                          assessment.lifecycleStatus ||
                          "Unavailable"}
                      </dd>
                    </div>
                    <div>
                      <dt>Retirement date</dt>
                      <dd>
                        {formatDate(
                          selectedTargetDeployment?.retirementDate ??
                            assessment.retirementDate,
                        )}
                      </dd>
                    </div>
                  </dl>
                </article>

                <article className="assessment-card">
                  <span className="section-kicker">API SURFACE</span>
                  <h2>Reported capabilities</h2>
                  {assessment.capabilities.length === 0 ? (
                    <p>No capability metadata was returned.</p>
                  ) : (
                    <div className="capability-list">
                      {assessment.capabilities.map((capability) => (
                        <Badge key={capability.name} appearance="tint" color="informative">
                          {capability.name}: {capability.value}
                        </Badge>
                      ))}
                    </div>
                  )}
                </article>

              </section>
            </>
          ) : null)}

          <footer>Assess validates the candidate before any workload changes are proposed.</footer>
        </main>
      </FluentProvider>
    );
  }

  if (selectedModel) {
    return (
      <FluentProvider theme={webLightTheme}>
        <main className="page-shell detail-page">
          <div className="ambient ambient-one" />
          <div className="ambient ambient-two" />

          <Button appearance="subtle" className="back-button" onClick={closeModel}>
            ← Back to deployments
          </Button>

          <WorkflowNavigation activeStep="discover" />

          <header className="hero detail-hero">
            <div className="eyebrow">
              <span className="eyebrow-mark" />
              MODEL MIGRATION · DETAILS
            </div>
            <div className="hero-row">
              <div>
                <h1>{selectedModel.deploymentName || "Model details"}</h1>
                <p>
                  Review the deployed model and Azure resource before starting a migration
                  assessment.
                </p>
              </div>
              <Button
                appearance="primary"
                size="large"
                onClick={() => void startAssessment()}
                disabled={recommendationLoading || !recommendation}
              >
                Migrate
              </Button>
            </div>
          </header>

          <section className="detail-layout">
            <article className="detail-panel">
              <div className="detail-panel-heading">
                <div>
                  <span className="section-kicker">SOURCE DEPLOYMENT</span>
                  <h2>
                    {selectedModel.modelName || "Unknown model"}{" "}
                    <span className="model-version">
                      {selectedModel.modelVersion || "Default version"}
                    </span>
                  </h2>
                </div>
                <StatusBadge model={selectedModel} />
              </div>

              <div className="detail-grid">
                <div>
                  <span>Deployment</span>
                  <strong>{selectedModel.deploymentName || "Unnamed deployment"}</strong>
                </div>
                <div>
                  <span>Model format</span>
                  <strong>{selectedModel.modelFormat || "Unknown"}</strong>
                </div>
                <div>
                  <span>Lifecycle</span>
                  <strong>{selectedModel.lifecycleStatus || "Unavailable"}</strong>
                </div>
                <div>
                  <span>Retirement date</span>
                  <strong>{formatDate(selectedModel.retirementDate)}</strong>
                </div>
                <div>
                  <span>Azure AI resource</span>
                  <strong>{selectedModel.accountName || "Unavailable"}</strong>
                </div>
                <div>
                  <span>Resource type</span>
                  <strong>{resourceTypeLabel(selectedModel.accountKind)}</strong>
                </div>
                <div>
                  <span>Resource group</span>
                  <strong>{selectedModel.resourceGroup || "Unavailable"}</strong>
                </div>
                <div>
                  <span>Region</span>
                  <strong>{selectedModel.location || "Unavailable"}</strong>
                </div>
                <div>
                  <span>Upgrade policy</span>
                  <strong>{selectedModel.versionUpgradeOption || "Not configured"}</strong>
                </div>
              </div>

              <div className="resource-id">
                <span>Deployment resource ID</span>
                <code>{selectedModel.resourceId || "Unavailable"}</code>
              </div>
            </article>

            <aside className="recommendation-panel" aria-live="polite">
              <span className="section-kicker">TARGET MODEL</span>
              {recommendationLoading ? (
                <div className="recommendation-loading">
                  <Spinner label="Finding a migration target…" />
                </div>
              ) : recommendation ? (
                <div className="recommendation-result">
                  <span className="recommendation-label">Suggested model</span>
                  <strong>{recommendation.suggestedModel}</strong>
                </div>
              ) : (
                <div className="recommendation-placeholder">
                  <Cube20Regular />
                  <h2>Loading suggestion</h2>
                  <p>The default target recommendation will appear here.</p>
                </div>
              )}

              {recommendationError && (
                <MessageBar intent="error">
                  <MessageBarBody>{recommendationError}</MessageBarBody>
                </MessageBar>
              )}
            </aside>
          </section>

          <footer>Review the source and suggested target before continuing.</footer>
        </main>
      </FluentProvider>
    );
  }

  return (
    <FluentProvider theme={webLightTheme}>
      <main className="page-shell">
        <div className="ambient ambient-one" />
        <div className="ambient ambient-two" />

        <header className="hero">
          <div className="eyebrow">
            <span className="eyebrow-mark" />
            AZURE DEVELOPER CLI
          </div>
          <div className="hero-row">
            <div>
              <h1>Model migration</h1>
              <p>
                Find deployed model versions approaching retirement and start with the
                workloads that carry the most urgency.
              </p>
            </div>
            <Button
              appearance="subtle"
              icon={<ArrowClockwise20Regular />}
              onClick={() => void loadModels()}
              disabled={loading}
            >
              Refresh inventory
            </Button>
          </div>
        </header>

        <WorkflowNavigation activeStep="discover" />

        <section className="summary-grid" aria-label="Deployment summary">
          <article className="summary-card total">
            <Cube20Regular />
            <div>
              <span>Deployments discovered</span>
              <strong>{loading ? "—" : summary.total}</strong>
            </div>
          </article>
          <article className="summary-card urgent">
            <Warning20Regular />
            <div>
              <span>Retired or within 90 days</span>
              <strong>{loading ? "—" : summary.urgent}</strong>
            </div>
          </article>
          <article className="summary-card upcoming">
            <CalendarClock20Regular />
            <div>
              <span>Retiring within one year</span>
              <strong>{loading ? "—" : summary.upcoming}</strong>
            </div>
          </article>
        </section>

        <section className="inventory-panel">
          <div className="inventory-heading">
            <div>
              <span className="section-kicker">DISCOVER</span>
              <h2>Deployed models</h2>
              <p>
                Subscription <code>{data?.subscriptionId ?? "Loading…"}</code>
              </p>
            </div>
            <div className="controls">
              <Input
                contentBefore={<Search20Regular />}
                value={query}
                onChange={(_, value) => setQuery(value.value)}
                placeholder="Search deployment, model, resource…"
                aria-label="Search deployments"
              />
              <Button
                appearance={attentionOnly ? "primary" : "secondary"}
                onClick={() => setAttentionOnly((current) => !current)}
              >
                Needs attention
              </Button>
            </div>
          </div>

          {data?.warnings?.map((warning) => (
            <MessageBar key={warning} intent="warning">
              <MessageBarBody>{warning}</MessageBarBody>
            </MessageBar>
          ))}

          {error && (
            <MessageBar intent="error">
              <MessageBarBody>{error}</MessageBarBody>
            </MessageBar>
          )}

          {loading ? (
            <div className="loading-state">
              <Spinner size="large" label="Loading Azure AI resources…" />
            </div>
          ) : filteredModels.length === 0 && incompleteResourceScans.length === 0 ? (
            <div className="empty-state">
              <Cube20Regular />
              <h3>No deployments found</h3>
              <p>
                Try another search or confirm that this subscription contains Azure OpenAI
                or Microsoft Foundry model deployments.
              </p>
            </div>
          ) : (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Deployment</th>
                    <th>Model version</th>
                    <th>Azure resource</th>
                    <th>Lifecycle</th>
                    <th>Retirement date</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredModels.map((model) => (
                    <tr key={model.resourceId || `${model.accountName}/${model.deploymentName}`}>
                      <td>
                        <strong>{model.deploymentName || "Unnamed deployment"}</strong>
                        <span>{model.location || "Location unavailable"}</span>
                      </td>
                      <td>
                        <strong>{model.modelName || "Unknown model"}</strong>
                        <span>
                          {model.modelVersion || "Default version"} ·{" "}
                          {model.modelFormat || "Unknown format"}
                        </span>
                      </td>
                      <td>
                        <strong>{model.accountName}</strong>
                        <span>
                          {resourceTypeLabel(model.accountKind)} · {model.resourceGroup}
                        </span>
                      </td>
                      <td>
                        <StatusBadge model={model} />
                        {model.versionUpgradeOption && (
                          <span className="upgrade-policy">{model.versionUpgradeOption}</span>
                        )}
                      </td>
                      <td>
                        <strong>{formatDate(model.retirementDate)}</strong>
                        <span>
                          {model.retirementDate
                            ? daysUntil(model.retirementDate) >= 0
                              ? `${daysUntil(model.retirementDate)} days remaining`
                              : "Retirement date passed"
                            : "Lifecycle metadata unavailable"}
                        </span>
                      </td>
                      <td>
                        <Button appearance="secondary" onClick={() => openModel(model)}>
                          View details
                        </Button>
                      </td>
                    </tr>
                  ))}
                  {incompleteResourceScans.map((scan) => (
                    <tr className="resource-scan-row" key={`scan-${scan.resource.resourceId}`}>
                      <td colSpan={6}>
                        <div>
                          {scan.status === "pending" ? (
                            <Spinner size="tiny" />
                          ) : (
                            <Warning20Regular />
                          )}
                          <span>
                            <strong>{scan.resource.name}</strong>
                            <small>
                              {resourceTypeLabel(scan.resource.kind)} ·{" "}
                              {scan.resource.resourceGroup} · {scan.resource.location}
                            </small>
                          </span>
                          <em>
                            {scan.status === "pending"
                              ? "Scanning deployments…"
                              : scan.message || "Deployment scan was incomplete."}
                          </em>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        <footer>Lifecycle metadata is reported by the Azure Cognitive Services models API.</footer>
      </main>
    </FluentProvider>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
