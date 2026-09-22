import { useEffect, useId, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { Badge, Button, MessageBar, MessageBarBody, Spinner } from "@fluentui/react-components";
import { EvidenceNotes } from "./EvidenceNotes";
import {
  canConfirmMapping,
  fieldsForLane,
  mappingSchemaPayload,
  validateEvaluationFiles,
  type ConfirmedMapping,
  type DatasetMapping,
  type EvaluationMapping,
  type MappingField,
  type MappingDraft,
  type MappingFilter,
  type MappingLane,
  type MappingPreview,
  type MappingProfile,
} from "./evaluationMapping";

type Props = {
  token: string;
  prompt: { file: File; sha256: string } | null;
  sourceModel: string;
  targetModel: string;
  contextKey: string;
  optimizer: { accountName: string; modelName: string; deploymentName: string } | null;
  onConfirm: (evidence: ConfirmedMapping | null) => void;
  draft: MappingDraft;
  onDraftChange: Dispatch<SetStateAction<MappingDraft>>;
};

function PathField({
  label, value, fields, disabled, onChange,
}: {
  label: string;
  value: string;
  fields: MappingField[];
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  const id = useId();
  const field = fields.find((item) => item.path === value);
  return (
    <label className="mapping-field">
      <span>{label}</span>
      <input
        list={id}
        aria-label={label}
        aria-describedby={field?.sample !== undefined ? `${id}-sample` : undefined}
        value={value}
        disabled={disabled}
        placeholder="/field/path"
        onChange={(event) => onChange(event.target.value)}
        spellCheck={false}
      />
      <datalist id={id}>
        {fields.map((item) => (
          <option key={item.path} value={item.path}>{item.types.join(" / ")}</option>
        ))}
      </datalist>
      {field?.sample !== undefined && (
        <small id={`${id}-sample`} title={field.sample}>Sample: {field.sample}</small>
      )}
    </label>
  );
}

function FilterEditor({
  label, filters, fields, disabled, onChange,
}: {
  label: string;
  filters: MappingFilter[];
  fields: MappingField[];
  disabled: boolean;
  onChange: (filters: MappingFilter[]) => void;
}) {
  return (
    <div className="mapping-filters">
      <div className="mapping-inline-heading">
        <strong>{label}</strong>
        {!disabled && (
          <Button
            size="small"
            onClick={() => onChange([...filters, { path: "", value: "" }])}
          >
            Add condition
          </Button>
        )}
      </div>
      {filters.length === 0 && <small>All records in the selected collection.</small>}
      {filters.map((filter, index) => (
        <div className="mapping-filter-row" key={index}>
          <PathField
            label={`${label} field ${index + 1}`}
            fields={fields}
            value={filter.path}
            disabled={disabled}
            onChange={(path) =>
              onChange(filters.map((item, i) => i === index ? { ...item, path } : item))
            }
          />
          <label className="mapping-field">
            <span>Equals</span>
            <input
              aria-label={`${label} value ${index + 1}`}
              value={filter.value}
              disabled={disabled}
              onChange={(event) => onChange(filters.map((item, i) =>
                i === index ? { ...item, value: event.target.value } : item,
              ))}
            />
          </label>
          {!disabled && (
            <Button
              size="small"
              aria-label={`Remove ${label.toLowerCase()} condition ${index + 1}`}
              onClick={() => onChange(filters.filter((_, i) => i !== index))}
            >
              Remove
            </Button>
          )}
        </div>
      ))}
    </div>
  );
}

function LaneEditor({
  role, lane, profile, disabled, onChange,
}: {
  role: "Source" | "Target";
  lane: MappingLane;
  profile: MappingProfile;
  disabled: boolean;
  onChange: (lane: MappingLane) => void;
}) {
  const fields = fieldsForLane(profile, lane);
  const file = profile.files.find((item) => item.index === lane.file);
  const evaluatorFields = lane.evaluator.collection
    ? fields
      .filter((field) => field.path.startsWith(`${lane.evaluator.collection}/0/`))
      .map((field) => ({
        ...field,
        path: field.path.slice(`${lane.evaluator.collection}/0`.length),
      }))
    : fields;
  return (
    <article className={`mapping-lane mapping-lane-${role.toLowerCase()}`}>
      <div className="mapping-inline-heading">
        <h4>{role}</h4>
        <span className="mapping-lane-caption">Observation + quality grade</span>
      </div>
      <div className="mapping-fields">
        <label className="mapping-field">
          <span>{role} file</span>
          <select
            aria-label={`${role} file`}
            value={lane.file}
            disabled={disabled}
            onChange={(event) => {
              const next = profile.files.find((item) => item.index === Number(event.target.value));
              onChange({ ...lane, file: Number(event.target.value), collection: next?.collections[0]?.path ?? "" });
            }}
          >
            <option value={-1}>Select {role} file</option>
            {profile.files.map((item) => (
              <option key={item.index} value={item.index}>{item.name}</option>
            ))}
          </select>
        </label>
        {(file?.collections.length ?? 0) > 1 && (
          <label className="mapping-field">
            <span>{role} record collection</span>
            <select
              aria-label={`${role} record collection`}
              value={lane.collection}
              disabled={disabled}
              onChange={(event) => onChange({ ...lane, collection: event.target.value })}
            >
              {file?.collections.map((item) => (
                <option key={item.path} value={item.path}>
                  {item.path || "Root records"} ({item.rowCount})
                </option>
              ))}
            </select>
          </label>
        )}
        {([
          ["caseId", "Case ID / join key"],
          ["input", "Input"],
          ["output", "Model output"],
          ["model", "Reported model (optional)"],
        ] as const).map(([key, label]) => (
          <PathField
            key={key}
            label={`${role} ${label}`}
            fields={fields}
            value={lane[key]}
            disabled={disabled}
            onChange={(value) => onChange({ ...lane, [key]: value })}
          />
        ))}
      </div>
      <FilterEditor
        label={`${role} record selection`}
        filters={lane.filters}
        fields={fields}
        disabled={disabled}
        onChange={(filters) => onChange({ ...lane, filters })}
      />
      <div className="mapping-grade">
        <h5>Quality policy</h5>
        <PathField
          label={`${role} evaluator array (blank = fields on the record)`}
          fields={fields}
          value={lane.evaluator.collection}
          disabled={disabled}
          onChange={(collection) => onChange({
            ...lane, evaluator: { ...lane.evaluator, collection },
          })}
        />
        {lane.evaluator.collection && (
          <FilterEditor
            label={`${role} evaluator selection`}
            fields={evaluatorFields}
            filters={lane.evaluator.filters}
            disabled={disabled}
            onChange={(filters) => onChange({
              ...lane, evaluator: { ...lane.evaluator, filters },
            })}
          />
        )}
        <div className="mapping-fields">
          <label className="mapping-field">
            <span>{role} pass rule</span>
            <select
              aria-label={`${role} pass rule`}
              value={lane.passRule}
              disabled={disabled}
              onChange={(event) => onChange({
                ...lane, passRule: event.target.value === "threshold" ? "threshold" : "reported",
              })}
            >
              <option value="reported">Reported pass / fail</option>
              <option value="threshold">Score threshold</option>
            </select>
          </label>
          <PathField
            label={`${role} ${lane.passRule === "threshold" ? "Score" : "Pass / fail"} field`}
            fields={evaluatorFields}
            value={lane.passRule === "threshold" ? lane.evaluator.score : lane.evaluator.status}
            disabled={disabled}
            onChange={(value) => onChange({
              ...lane,
              evaluator: { ...lane.evaluator, [lane.passRule === "threshold" ? "score" : "status"]: value },
            })}
          />
          {lane.passRule === "threshold" && (
            <>
              <label className="mapping-field">
                <span>{role} score direction</span>
                <select
                  aria-label={`${role} score direction`}
                  value={lane.operator}
                  disabled={disabled}
                  onChange={(event) => onChange({
                    ...lane, operator: event.target.value === "lte" ? "lte" : "gte",
                  })}
                >
                  <option value="gte">Pass when score ≥ threshold</option>
                  <option value="lte">Pass when score ≤ threshold</option>
                </select>
              </label>
              <label className="mapping-field">
                <span>{role} threshold</span>
                <input
                  type="number"
                  step="any"
                  value={lane.threshold ?? ""}
                  disabled={disabled}
                  placeholder="Required — no inferred default"
                  onChange={(event) => onChange({
                    ...lane,
                    threshold: event.target.value === "" ? undefined : event.target.valueAsNumber,
                  })}
                />
              </label>
            </>
          )}
          {lane.passRule === "reported" && (
            <PathField
              label={`${role} score field (optional)`}
              fields={evaluatorFields}
              value={lane.evaluator.score}
              disabled={disabled}
              onChange={(score) => onChange({ ...lane, evaluator: { ...lane.evaluator, score } })}
            />
          )}
          <PathField
            label={`${role} evaluator reason (optional)`}
            fields={evaluatorFields}
            value={lane.evaluator.rationale}
            disabled={disabled}
            onChange={(rationale) => onChange({ ...lane, evaluator: { ...lane.evaluator, rationale } })}
          />
        </div>
      </div>
      <details className="mapping-advanced">
        <summary>Optional context and reference evidence</summary>
        <p className="mapping-help">
          Map only evidence actually present in the export. Leave missing fields blank.
          These values can be included in the optimization request after separate consent.
        </p>
        <div className="mapping-fields">
          {([
            ["context", "runtime context"],
            ["expectedOutput", "expected output / label"],
            ["expectedReferences", "expected references"],
          ] as const).map(([key, label]) => (
            <PathField
              key={key}
              label={`${role} ${label}`}
              fields={fields}
              value={lane[key] ?? ""}
              disabled={disabled}
              onChange={(value) => onChange({ ...lane, [key]: value })}
            />
          ))}
        </div>
      </details>
    </article>
  );
}

export function EvaluationMappingIntake({
  token, prompt, sourceModel, targetModel, contextKey, optimizer, onConfirm, draft, onDraftChange,
}: Props) {
  const { files, profile, mapping, preview, acknowledged, confirmed, origin } = draft;
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [dragging, setDragging] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const requestId = useRef(0);
  const controllerRef = useRef<AbortController | null>(null);
  const operationRef = useRef<"profile" | "preview" | "propose" | null>(null);
  const updateDraft = (patch: Partial<MappingDraft>) =>
    onDraftChange((current) => ({ ...current, ...patch }));

  const invalidate = () => {
    requestId.current += 1;
    controllerRef.current?.abort();
    operationRef.current = null;
    setBusy("");
    updateDraft({ preview: null, acknowledged: false, confirmed: false });
    onConfirm(null);
  };

  useEffect(() => {
    if (draft.contextKey !== contextKey) {
      // Profiling reads file structure, so finishing a concurrent prompt upload must not cancel it.
      if (operationRef.current !== "profile") {
        requestId.current += 1;
        controllerRef.current?.abort();
        operationRef.current = null;
        setBusy("");
      }
      onDraftChange((current) => ({
        ...current, contextKey, preview: null, acknowledged: false, confirmed: false,
      }));
      onConfirm(null);
    }
  }, [contextKey, draft.contextKey, onConfirm, onDraftChange]);

  useEffect(() => () => {
    requestId.current += 1;
    controllerRef.current?.abort();
  }, []);

  const request = async <T,>(
    operation: "profile" | "preview" | "propose",
    currentFiles: File[],
    currentMapping?: EvaluationMapping,
  ): Promise<T | null> => {
    controllerRef.current?.abort();
    const id = ++requestId.current;
    const controller = new AbortController();
    controllerRef.current = controller;
    operationRef.current = operation;
    const timeout = window.setTimeout(() => controller.abort(), operation === "propose" ? 95_000 : 60_000);
    setError("");
    setBusy(operation);
    try {
      const form = new FormData();
      currentFiles.forEach((file) => form.append("files", file, file.name));
      if (prompt) form.append("prompt", prompt.file, prompt.file.name);
      form.append("sourceModelName", sourceModel);
      form.append("targetModelName", targetModel);
      if (currentMapping) form.append("mapping", JSON.stringify(currentMapping));
      if (operation === "propose" && optimizer) {
        form.append("optimizerAccountName", optimizer.accountName);
        form.append("optimizerModelName", optimizer.modelName);
        form.append("optimizerDeploymentName", optimizer.deploymentName);
      }
      const response = await fetch(`/api/evaluation-mapping/${operation}`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
        body: form,
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error ?? `Mapping request failed (${response.status}).`);
      }
      return id === requestId.current ? payload as T : null;
    } catch (requestError) {
      if (id === requestId.current) {
        setError(requestError instanceof DOMException && requestError.name === "AbortError"
          ? "The mapping request timed out. Your files and edits are retained; retry or map manually."
          : requestError instanceof Error ? requestError.message : String(requestError));
      }
      return null;
    } finally {
      window.clearTimeout(timeout);
      if (id === requestId.current) {
        operationRef.current = null;
        setBusy("");
      }
    }
  };

  const loadFiles = async (nextFiles: File[]) => {
    const issue = nextFiles.length ? validateEvaluationFiles(nextFiles, prompt?.file.size) : null;
    if (issue) {
      setError(issue);
      return;
    }
    invalidate();
    setError("");
    updateDraft({ files: nextFiles, profile: null, mapping: null });
    if (!nextFiles.length) return;
    const result = await request<MappingProfile>("profile", nextFiles);
    if (result) {
      updateDraft({
        profile: result,
        mapping: result.mapping,
        origin: optimizer ? "Local fallback while AI maps the schema" : "Detected locally — AI unavailable",
      });
      if (optimizer) {
        const proposed = await request<MappingProfile>("propose", nextFiles);
        if (proposed) {
          updateDraft({
            mapping: proposed.mapping,
            profile: proposed,
            origin: "AI suggestion — review required",
          });
        }
      }
    }
  };

  const addFiles = (selected: File[]) => {
    if (!selected.length) return;
    if (selected.some((candidate) => files.some((file) =>
      file.name === candidate.name && file.size === candidate.size && file.lastModified === candidate.lastModified,
    ))) {
      setError("That file is already added. Remove it first if you want to replace it.");
      return;
    }
    void loadFiles([...files, ...selected]);
  };

  const editMapping = (next: EvaluationMapping) => {
    invalidate();
    setError("");
    updateDraft({ mapping: next, origin: "Edited by you" });
  };

  const previewMapping = async () => {
    if (!mapping) return;
    const issue = validateEvaluationFiles(files, prompt?.file.size);
    if (issue) {
      setError(issue);
      return;
    }
    invalidate();
    const result = await request<MappingPreview>("preview", files, mapping);
    if (result) updateDraft({ preview: result });
  };

  const proposeMapping = async () => {
    if (!profile || !optimizer) return;
    invalidate();
    const result = await request<MappingProfile>("propose", files);
    if (result) {
      updateDraft({ mapping: result.mapping, profile: result, origin: "AI suggestion — review required" });
    }
  };

  const native = mapping !== null && mapping.adapter !== "generic";
  const editingDisabled = native || busy !== "";
  const datasetFile = profile?.files.find((file) => file.index === mapping?.dataset?.file);
  const datasetFields = datasetFile?.collections.find((item) =>
    item.path === mapping?.dataset?.collection,
  )?.fields ?? [];

  return (
    <div className="mapping-intake">
      <div
        className={`mapping-dropzone ${dragging ? "dragging" : ""}`}
        onDragOver={(event) => { event.preventDefault(); if (!busy) setDragging(true); }}
        onDragLeave={() => setDragging(false)}
        onDrop={(event) => {
          event.preventDefault();
          setDragging(false);
          if (!busy) addFiles(Array.from(event.dataTransfer.files));
        }}
      >
        <div className="mapping-dropzone-heading">
          <div>
            <span className="section-kicker">01 / EVALUATION DATA</span>
            <h3>{files.length ? "Your evidence, in any supported layout" : "Drop 1–3 evaluation files here"}</h3>
            <p>Separate Source and Target results are recommended; add a dataset if available.
              One comparison file also works.</p>
          </div>
          <Badge appearance="tint" color={files.length ? "informative" : "subtle"}>
            {files.length} / 3 files
          </Badge>
        </div>
        <input
          ref={inputRef}
          type="file"
          accept=".json,.jsonl,.csv,.xlsx"
          multiple
          hidden
          onChange={(event) => {
            addFiles(Array.from(event.target.files ?? []));
            event.target.value = "";
          }}
        />
        <div className="mapping-upload-actions">
          <Button
            appearance="primary"
            disabled={files.length >= 3 || busy !== ""}
            onClick={() => inputRef.current?.click()}
          >
            {files.length ? "Add files" : "Choose evaluation files"}
          </Button>
          <span>JSON · JSONL · CSV · XLSX / less than 24 MB including prompt</span>
        </div>
      </div>

      {files.length > 0 && (
        <ol className="mapping-file-list" aria-label="Uploaded evaluation files">
          {files.map((file, index) => (
            <li key={`${index}:${file.name}`}>
              <span className="mapping-file-index">{String(index + 1).padStart(2, "0")}</span>
              <div>
                <strong>{file.name}</strong>
                <small>
                  {(file.size / 1024).toFixed(1)} KB
                  {profile?.files[index] && ` · ${profile.files[index].format.toUpperCase()}`}
                </small>
              </div>
              <Button
                size="small"
                disabled={busy !== ""}
                aria-label={`Remove ${file.name}`}
                onClick={() => void loadFiles(files.filter((_, i) => i !== index))}
              >
                Remove
              </Button>
            </li>
          ))}
        </ol>
      )}

      {error && <MessageBar intent="error"><MessageBarBody>{error}</MessageBarBody></MessageBar>}
      {!profile && files.length > 0 && !busy && (
        <Button onClick={() => void loadFiles(files)}>Retry reading files</Button>
      )}
      {busy && (
        <div className="analysis-loading" role="status">
          <Spinner label={busy === "profile" ? "Reading file structures locally…"
            : busy === "propose" ? "Suggesting a mapping from the schema only…"
              : "Joining and validating all records…"} />
          <Button
            onClick={() => {
              invalidate();
              setError("Request cancelled. Your files and any existing mapping edits are retained.");
            }}
          >
            Cancel request
          </Button>
        </div>
      )}

      {profile && mapping && (
        <section className="mapping-review" aria-label="Evaluation mapping">
          <div className="mapping-inline-heading">
            <div>
              <span className="section-kicker">02 / REVIEW MAPPING</span>
              <h3>Make the comparison explicit</h3>
            </div>
            <Badge appearance="tint" color="informative">{origin}</Badge>
          </div>
          <p className="mapping-help">
            Select the two roles, case key, and quality grade. One file can supply both roles.
            Paths use JSON Pointer syntax; <code>/sample.output_text</code> is a literal dotted key.
          </p>
          <EvidenceNotes notes={profile.warnings ?? []} title="Mapping notes" />
          {native && (
            <div className="mapping-native-notice">
              <div>
                <strong>{mapping.adapter === "foundry" ? "Foundry bundle"
                  : mapping.adapter === "meera" ? "Meera workbook" : "Canonical evidence"} adapter</strong>
                <span>Known format: preserves its existing evaluator and lineage rules. Preview before accepting.</span>
              </div>
              <Button
                disabled={busy !== ""}
                onClick={() => editMapping({ ...mapping, adapter: "generic" })}
              >
                Customize field mapping
              </Button>
            </div>
          )}
          <div className="mapping-lanes">
            <LaneEditor
              role="Source" lane={mapping.source} profile={profile} disabled={editingDisabled}
              onChange={(source) => editMapping({ ...mapping, source })}
            />
            <LaneEditor
              role="Target" lane={mapping.target} profile={profile} disabled={editingDisabled}
              onChange={(target) => editMapping({ ...mapping, target })}
            />
          </div>
          <details className="mapping-dataset" open={Boolean(mapping.dataset)}>
            <summary>Dataset / reference attachment {mapping.dataset ? "(linked)" : "(optional)"}</summary>
            <label className="mapping-field">
              <span>Dataset file</span>
              <select
                aria-label="Dataset file"
                value={mapping.dataset?.file ?? ""}
                disabled={editingDisabled}
                onChange={(event) => {
                  const file = profile.files.find((item) => item.index === Number(event.target.value));
                  editMapping({
                    ...mapping,
                    dataset: event.target.value === "" ? undefined : {
                      file: Number(event.target.value), collection: file?.collections[0]?.path ?? "",
                      caseId: "", input: "", reference: "",
                    },
                  });
                }}
              >
                <option value="">No additional dataset</option>
                {profile.files.map((file) => <option key={file.index} value={file.index}>{file.name}</option>)}
              </select>
            </label>
            {mapping.dataset && (
              <div className="mapping-fields">
                {(datasetFile?.collections.length ?? 0) > 1 && (
                  <label className="mapping-field">
                    <span>Dataset record collection</span>
                    <select
                      aria-label="Dataset record collection"
                      value={mapping.dataset.collection}
                      disabled={editingDisabled}
                      onChange={(event) => {
                        if (mapping.dataset) editMapping({
                          ...mapping, dataset: { ...mapping.dataset, collection: event.target.value },
                        });
                      }}
                    >
                      {datasetFile?.collections.map((collection) => (
                        <option key={collection.path} value={collection.path}>
                          {collection.path || "Root records"}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                {(["caseId", "input", "reference", "expectedOutput"] as const).map((key) => (
                  <PathField
                    key={key}
                    label={`Dataset ${key === "caseId" ? "case ID" : key === "expectedOutput"
                      ? "expected output / label (optional)" : key}`}
                    fields={datasetFields}
                    value={mapping.dataset?.[key] ?? ""}
                    disabled={editingDisabled}
                    onChange={(value) => {
                      if (mapping.dataset) editMapping({
                        ...mapping, dataset: { ...mapping.dataset, [key]: value } satisfies DatasetMapping,
                      });
                    }}
                  />
                ))}
              </div>
            )}
          </details>
          <div className="mapping-ai">
            <div className="mapping-inline-heading">
              <div>
                <strong>Schema-only AI mapping</strong>
                <p>{optimizer
                  ? `Suggested automatically by ${optimizer.modelName}; review and adjust before preview.`
                  : "No mapping model is available; review the local fallback manually."}</p>
              </div>
              {optimizer && (
                <Button disabled={busy !== ""} onClick={() => void proposeMapping()}>
                  Regenerate suggestion
                </Button>
              )}
            </div>
            {optimizer && (
              <details>
                <summary>View the schema payload sent automatically</summary>
                <p>
                  Only field paths and types are sent to {optimizer.accountName} / {optimizer.deploymentName}.
                  Presence/distinct counts help identify stable keys. No raw records, prompts, outputs,
                  filenames, hashes, or sample values are included.
                </p>
                <pre>{JSON.stringify(mappingSchemaPayload(profile), null, 2)}</pre>
              </details>
            )}
          </div>
          <div className="mapping-preview-action">
            <span>Mapping only reads your data. It does not rerun evaluations or change scores.</span>
            <Button
              appearance="primary"
              disabled={!prompt || busy !== ""}
              onClick={() => void previewMapping()}
            >
              Preview mapped evidence
            </Button>
          </div>
          {!prompt && <p className="mapping-help">Upload the Source prompt to preview and confirm this comparison.</p>}
        </section>
      )}

      {preview && mapping && (
        <section className="mapping-preview" aria-label="Mapped evidence preview">
          <div className="mapping-inline-heading">
            <div>
              <span className="section-kicker">03 / PREVIEW & CONFIRM</span>
              <h3>{preview.valid ? "Review the normalized comparison" : "Resolve mapping issues"}</h3>
            </div>
            <Badge appearance="tint" color={confirmed ? "success" : preview.valid ? "informative" : "danger"}>
              {confirmed ? "Mapping confirmed" : preview.valid ? "Awaiting your confirmation" : "Not ready"}
            </Badge>
          </div>
          <div className="mapping-counts">
            {[
              ["Paired cases", preview.caseCount],
              ["Comparable grades", preview.comparableCount],
              ["Unclassified", preview.unclassifiedCount],
              ["Source only", preview.sourceOnly],
              ["Target only", preview.targetOnly],
            ].map(([label, count]) => (
              <div key={label}><span>{label}</span><strong>{count}</strong></div>
            ))}
          </div>
          {preview.issues.filter((issue) => issue.severity === "error").map((issue, index) => (
            <MessageBar key={index} intent="error">
              <MessageBarBody>{issue.caseId ? `${issue.caseId}: ` : ""}{issue.message}</MessageBarBody>
            </MessageBar>
          ))}
          <EvidenceNotes notes={preview.issues.filter((issue) => issue.severity === "warning").map(
            (issue) => `${issue.caseId ? `${issue.caseId}: ` : ""}${issue.message}`,
          )} />
          <div className="mapping-preview-table-wrap">
            <table className="mapping-preview-table">
              <caption>
                Showing {preview.cases.length} of {preview.caseCount} pairs. All records are validated.
                Missing or errored grades stay unclassified.
              </caption>
              <thead>
                <tr><th>Case / input</th><th>Source</th><th>Target</th></tr>
              </thead>
              <tbody>
                {preview.cases.map((item) => (
                  <tr key={item.caseId}>
                    <td><strong>{item.caseId}</strong><details><summary>Input</summary><pre>{item.input}</pre></details></td>
                    <td>
                      <span className={`mapping-status ${item.sourceStatus}`}>{item.sourceStatus}</span>
                      {item.sourceScore !== undefined && <small> Score: {item.sourceScore}</small>}
                      <details><summary>Output</summary><pre>{item.sourceOutput}</pre></details>
                    </td>
                    <td>
                      <span className={`mapping-status ${item.targetStatus}`}>{item.targetStatus}</span>
                      {item.targetScore !== undefined && <small> Score: {item.targetScore}</small>}
                      <details><summary>Output</summary><pre>{item.targetOutput}</pre></details>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <label className="mapping-checkbox">
            <input
              type="checkbox"
              checked={acknowledged}
              disabled={!preview.valid || confirmed}
              onChange={(event) => updateDraft({ acknowledged: event.target.checked })}
            />
            <span>
              I reviewed the Source/Target roles, case matching and comparable quality rules,
              including thresholds and any unclassified cases. These results used the uploaded Source prompt.
            </span>
          </label>
          <Button
            appearance="primary"
            disabled={confirmed || draft.contextKey !== contextKey || !canConfirmMapping(preview, acknowledged)}
            onClick={() => {
              if (draft.contextKey !== contextKey || !canConfirmMapping(preview, acknowledged)) return;
              updateDraft({ confirmed: true });
              onConfirm({ files, mapping, preview });
            }}
          >
            {confirmed ? "Confirmed — ready to analyze" : "Confirm mapping"}
          </Button>
        </section>
      )}
    </div>
  );
}
