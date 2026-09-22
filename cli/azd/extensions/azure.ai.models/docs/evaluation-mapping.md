# Reviewed evaluation mapping (local preview)

The Adapt page has a single **Evaluation data** area instead of Combined export
and Run bundle tabs. Upload the Source prompt separately, then add **1-3**
evaluation files in any order. Accepted containers are JSON, JSONL, CSV and
XLSX. The prompt is limited to 256 KB; all uploaded files together must fit
within the 24 MB request limit, including multipart metadata.

## Workflow

Discover and Assess allow the suggested Target or another model. The picker
lists discovered model/format combinations. Existing-deployment assessment uses
the actual selected deployment's name, format and version. The Source deployment
itself is excluded as a Target. Changing the Target clears downstream comparison
state; selecting a model is not a guarantee that it is available or deployed.

Adapt is divided into **Azure Monitor -> Evaluation data -> Prompt optimization**.
Only the current step is shown. Use Next/Back or the enabled step navigation;
uploads and results survive navigation within an unchanged comparison.
Monitor can be explicitly skipped when the selected deployments exist, so
missing telemetry permissions do not block offline evaluation. Changed time
ranges invalidate Monitor results and late responses cannot restore them.

Evaluation run timestamps, when present, become an explicit Monitor-window
choice rather than silently replacing an already reviewed time range.

Within the Evaluation data step:

1. Choose the unchanged Source prompt.
2. Add evaluation files. One file may contain both roles; two files may contain
   separate runs; a third may supply dataset inputs/references.
3. The app sends a schema-only payload to the available mapping model
   automatically and presents its general-purpose suggestion for review. The
   payload contains field paths/types plus presence/distinct counts: no records,
   prompts, outputs, filenames, hashes or sample values.
4. Select Source and Target file/record collections, their case ID, input and
   output fields, and the quality evaluator. For long-format data, add exact
   record-selection conditions to identify each role.
5. Choose reported pass/fail or supply a score threshold and direction for each
   side. Confirm that the selected quality rules are comparable.
6. Select **Preview mapped evidence**. Review the full-data counts, diagnostics
   and a bounded sample of normalized pairs.
7. Acknowledge the roles, case matching, prompt provenance and grading rules,
   then **Confirm mapping**. Analysis and PromptV2 use that same mapping.
8. After analysis, review the optimization payload prepared locally. Explicitly
   authorize disclosure of the Source prompt and case content, then select
   **Prompt optimize**. No inferred cause or prompt-fixability label is required.

Step 8 is reached through **Next: Prompt optimization** after analysis. The
request is prepared when that step is first opened, not while reviewing
evaluation results. Informational provenance/limitations are deduplicated in
collapsed notes instead of repeated warning banners. Blocking errors and
explicit content-send authorization remain visible.

Files and mapping edits remain available when navigating back to assessment
and returning to Adapt without changing the comparison. A browser reload
requires re-upload. Changing a file, prompt, field, role, filter or pass rule
invalidates the previous confirmation, analysis and optimized candidate.

## Mapping fields

Field paths use JSON Pointer syntax. `/input/question` refers to nested
properties; `/datasource_item/sample.output_text` refers to a literal
`sample.output_text` property. A blank record collection selects root records
when the file exposes a root collection.

Source and Target can select the same physical file but must identify distinct
observations or paired output fields. A case ID must uniquely pair observations;
the engine does not join by row order or fuzzy text similarity. Duplicate IDs,
unmatched cases and conflicting inputs require correction before confirmation.

An evaluator array has its own exact-match selectors. Its pass, score and
reason paths are relative to the selected evaluator element. Do not select the
first evaluator by index just because it happens to be first in a sample.

For score-only results, supply the threshold explicitly. Missing/error grades
are not ordinary quality failures. The preview reports unclassified cases
separately from comparable pass/fail pairs.

Optional lane paths `context`, `expectedOutput` and `expectedReferences` preserve
available runtime context, reference answers/labels and expected-reference
evidence. A dataset can provide `expectedOutput` alongside its existing
`reference` field. Leave unavailable fields blank; the optimizer must not assume
missing separate context means retrieval failed, since context may be embedded
in the Question/input. Independently supplied reference values must agree.

## Automatic schema-only AI suggestion

After profiling JSON, JSONL, CSV or XLSX locally, the app automatically asks the
available mapping model for a declarative general-purpose mapping. The payload
contains field paths/types and presence/distinct counts; raw records, prompt
text, outputs, sample values, filenames and hashes are excluded. The exact
payload remains visible in the review UI, and a failed/unavailable AI call
leaves the local fallback and manual editor usable.

The mapper proposes a declarative plan, not generated transformation code.
Its suggestion is editable and never confirms itself. A failed or unavailable
AI call leaves the manual editor usable. Mapping uses a separate model client,
not the PromptV2 optimization endpoint.

Run IDs, latency and answer-model token fields are no longer exposed in the
mapping editor. They are not required for the quality comparison, and Azure
Monitor remains the operational-metrics source.

## Evaluation-export caveats

- Promptfoo test configuration is not completed evaluation evidence. Prefer a
  completed JSON export. A single export may require selecting two different
  provider/prompt partitions.
- Overall success can include cost and latency assertions. Select a comparable
  quality evaluator rather than assuming every failed aggregate is a
  prompt-quality regression.
- Provider labels are not proof of actual model identity. Missing metadata
  remains visibly unverified; conflicting reported models must be resolved.
- Answer-model token usage must not include evaluator/judge usage.
- Importing grades does not establish a root cause. Evaluate shows factual
  comparison outcomes and evaluator feedback, not inferred fixability labels.
  Unknown causes and legacy non-prompt labels do not block optimization.
  Case evidence remains available in an internally scrollable, keyboard-focusable
  list so large result sets do not push optimization controls down a very long page.
- Relabeled demo evidence remains demo-only; mapping does not make its outputs
  genuine results from the relabeled model.

## Full-evidence prompt optimization

The local build includes **all** cases returned in `analysis.cases`: quality
regressions, residual Target failures and operational findings. These categories
stay distinct. There is no 20-case cap, per-field truncation, or inferred
prompt-fixability filter. Unclassified grades are not converted to failures.
With no classified findings, the action is labeled general prompt improvement
rather than regression repair.

The Source prompt is sent as `developer_message`. A fixed instruction plus a
JSON-encoded, explicitly untrusted evidence block is sent through PromptV2's
existing `requested_changes` field. Case evidence includes available inputs,
outputs, grades, evaluator feedback, reference values, context and operational
deltas. It does not promote imported text or heuristic failure labels into
trusted rewrite directives. Legacy analysis fields may remain in API responses
for compatibility, but do not determine this action's eligibility or steering.

Before any model call, `POST /api/prompt-optimization-preview` receives the same
authenticated multipart inputs and optimizer configuration as optimization.
It re-reads the evidence locally and returns the exact request object, counts,
byte size, local size limit, prompt/evaluation hashes and a `requestSha256`
binding the destination account and request content.

`POST /api/prompt-optimization` requires two additional multipart values:

| Field | Meaning |
|---|---|
| `allowEvaluationContent=true` | Explicit permission to send prompt/case content; independent from the automatic schema-only mapping request |
| `optimizationRequestSha256` | The reviewed preview's `requestSha256`; recomputed server-side before sending |

Changing evidence, mapping, prompt or optimizer destination requires a fresh
preview and consent. The result echoes `optimizationRequestSha256`.

The local outbound-request guard is **2 MiB of serialized JSON**, distinct from
the upload limit and from any service token limit. Oversized previews report
the full count and size with `withinLimit=false` and `request=null`; no partial
request is sent. Provider context-limit/rate-limit errors remain visible rather
than triggering hidden sampling.

The preview and its consent do not claim that all findings can be fixed by a
prompt. PromptV2 proposes a candidate, possibly unchanged. Customer reruns of
the same evaluator and dataset are still required before claiming improvements
or promoting the prompt. The GPT-5.2 optimizer compatibility setting and
non-target-specific response labeling remain unchanged.

## Validate, Roll out and Retire

Validate accepts one adapted Target rerun in the same mapped schema and joins it
to the confirmed baseline by case ID. It reports resolved, remaining, new,
preserved, unclassified, missing and extra cases. The rollout-review gate is
strict: no remaining/new failures, coverage gaps, unclassified cases or mapping
errors. A matching exported candidate-prompt hash can verify prompt provenance;
otherwise the result remains explicitly unverified.

Roll out is a handoff checklist only. It summarizes Source rollback, Target,
validation counts and customer-owned actions. It does not generate deployment
artifacts or edit application configuration, CI/CD, deployment settings or
traffic.

Retire queries the Source deployment's Azure Monitor usage for the selected 7-
or 30-day window. It is read-only and never deletes or disables a deployment.
Zero observed requests is only a readiness signal: diagnostic coverage,
scheduled clients, the rollback window and deployment-owner approval still need
independent confirmation.

## Implementation boundary

This first local slice is stateless: profile, proposal and preview are
authenticated multipart requests, and analysis/optimization re-read the original
files with the reviewed plan. The evidence digest includes interpretation rules
as well as file content. Neither operation trusts browser-submitted result counts.

It does not implement the design's persistent import store, reusable mapping
library, arbitrary transformation scripts, fuzzy/composite-key inference, or
new standalone `mme.case.v2` export format. Existing analysis remains behind
the fixed internal case representation. The distributed preview ZIP must be
rebuilt separately to include these local changes.
