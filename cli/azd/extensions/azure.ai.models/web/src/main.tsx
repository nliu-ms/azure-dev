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

const workflowSteps = ["Discover", "Assess", "Adapt", "Validate", "Roll out", "Retire"];

function WorkflowNavigation({ activeStep }: { activeStep: "discover" | "assess" }) {
  const activeIndex = activeStep === "discover" ? 0 : 1;
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
  const [activeStep, setActiveStep] = useState<"discover" | "assess">("discover");
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
  const assessmentRequestId = useRef(0);
  const deploymentOptionsRequestId = useRef(0);
  const inventoryRequestId = useRef(0);

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
      (model) =>
        (model.resourceId || `${model.accountName}/${model.deploymentName}`) ===
        selectedTargetDeploymentId,
    ) ?? null;
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
    void requestAssessment(deployment);
  };

  const chooseNewDeployment = () => {
    assessmentRequestId.current += 1;
    setDeploymentChoice("new");
    setSelectedTargetDeploymentId("");
    setAssessment(null);
    setAssessmentError("");
    setAssessmentLoading(false);
    const defaultRegion = createRegion || selectedModel?.location || regions[0] || "";
    if (defaultRegion) {
      void requestDeploymentOptions(defaultRegion);
    }
  };

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
              <Button appearance="primary" size="large" disabled>
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
                      onChange={(event) => setSelectedSKU(event.target.value)}
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
