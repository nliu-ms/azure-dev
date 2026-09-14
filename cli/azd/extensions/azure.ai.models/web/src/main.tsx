import React, { useEffect, useMemo, useState } from "react";
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
  models: ModelDeployment[];
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

function App() {
  const [data, setData] = useState<ModelList | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [attentionOnly, setAttentionOnly] = useState(false);

  const loadModels = async () => {
    setLoading(true);
    setError("");
    try {
      const response = await fetch("/api/models", {
        headers: { Authorization: `Bearer ${token}` },
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Request failed with status ${response.status}`);
      }
      setData(payload as ModelList);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : String(requestError));
    } finally {
      setLoading(false);
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
              <Spinner size="large" label="Scanning Azure AI model deployments…" />
            </div>
          ) : filteredModels.length === 0 ? (
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
                          {model.resourceGroup} · {model.accountKind}
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
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        <footer>
          <span>
            Lifecycle metadata is reported by the Azure Cognitive Services models API.
          </span>
          <a href="https://deerflow.tech" target="_blank" rel="noreferrer">
            Created By Deerflow
          </a>
        </footer>
      </main>
    </FluentProvider>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
