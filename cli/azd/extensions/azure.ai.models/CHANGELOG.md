# Release History


## 0.0.7-preview (Unreleased)

### Features

- Added `azd ai models migrate`, which opens a local React experience for reviewing deployed model versions,
  lifecycle status, and retirement dates across Azure OpenAI and Foundry resources in a subscription
- Added a six-stage migration workflow. Discover compares the source deployment with an explicitly labeled
  GPT-5.4 default target, while Assess finds existing GPT-5.4 deployments, lets users choose an existing or
  new deployment path, and shows target lifecycle and capabilities from Azure management APIs
- Improved large-subscription discovery with bounded parallel account scans, per-account and overall
  timeouts, partial-result warnings, and an explicit browser timeout instead of an indefinite loading state
- Changed deployment discovery to load Azure AI resources first and scan each resource independently, so
  completed deployments appear immediately while pending or failed resources remain as compact table rows
  instead of producing a page of warning banners
- Fixed a blank page during progressive discovery when an Azure AI resource returned no deployments;
  empty model collections now remain JSON arrays and the browser safely handles nullable responses
- Simplified new deployment to one action; selecting it now hides existing deployments and lets users choose
  a region and SKU while checking current model capacity and quota signals from Azure management APIs
- Fixed regional SKU discovery to use the subscription model catalog for the selected region instead of the
  source resource catalog; online Data Zone and regional deployment types now appear when supported
- Added the Adapt step with inherited Source/Target deployments, Source Prompt and evaluation-result uploads,
  and local XLSX/JSON/JSONL regression analysis. The report compares evaluator pass rates, groups
  customer-provided failure evidence, identifies quality and operational regressions, and shows case-level
  evidence. Prompt-fixable regressions can now be sent to PromptV2 with Azure identity authentication to generate
  an adapted prompt, review a line-level diff and change rationale, and download the candidate for validation.
  PromptV2 uses `gpt-5.2` as a temporary compatibility target when the selected migration model is newer than the
  optimizer's supported target enum; the migration target and deployment remain unchanged and the result is labeled
  non-target-specific.
  Live Azure Monitor data remains an independent enrichment over a selected UTC window, with Source/Target summary
  metrics and time-series charts for TTFT, TBT, TTLT, Input/Output tokens, and request rate
- Adapt now inherits and displays the Source/Target pair fixed in Discover and Assess instead of asking users
  to select the same deployments again
- Labeled `OpenAI` resources as Azure OpenAI resources and `AIServices` resources as Foundry resources;
  Foundry Projects are not used as deployment ownership or inventory boundaries
- Added LoRA adapter support to `create` command with `--lora-rank`, `--lora-alpha`, `--lora-target-modules`, and `--lora-dropout` flags for registering LoRA adapters (`--weight-type LoRA`)
- `show` command now displays LoRA Configuration section (rank, alpha, target modules, dropout) for LoRA adapters
- `list` command now shows Weight Type column to distinguish FullWeight and LoRA models
- `--base-model` is now optional when `--weight-type` is `DraftModel` (still required for `FullWeight` and `LoRA`); `DraftModel` is also now documented in the `--weight-type` help text
- `--lora-rank` and `--lora-alpha` are now optional for `--weight-type LoRA`; when omitted, the values are read from the uploaded `adapter_config.json` by the Model Registry Service

## 0.0.6-preview (Unreleased)

### Features

- Added top-level `azd ai models create`, `list`, `show`, `delete` commands as the preferred surface; the `custom` subgroup is now deprecated
- Added `--weight-type` flag to `create` command (default: `FullWeight`)
- Added `--source-job-id` filter to `list` command for querying models by training job lineage
- Added `azd ai models update` command for updating model description and tags (JSON Merge Patch)
- `show` command now displays weight type, provisioning state, source lineage, and artifact profile when available
- `--publisher` flag is now optional (previously defaulted to `Fireworks`); only sent when explicitly provided

### Breaking Changes

- Removed `-e` shorthand for `--project-endpoint`; use `--project-endpoint` instead. This resolves a collision with the azd global `-e/--environment` flag.

### Improvements

- `startPendingUpload` request now sends `pendingUploadType: "TemporaryBlobReference"` for explicit upload type declaration
- Model response now supports new fields: `weightType`, `baseModel`, `source`, `artifactProfile`, `provisioningState`

### Deprecations

- `azd ai models custom <command>` is deprecated; use `azd ai models <command>` directly instead

## 0.0.5-preview (2026-03-24)

- Deprecated `-e` shorthand for `--project-endpoint`; use the full flag name instead
- Improved error handling for 403 (Forbidden) during `custom create` upload, with guidance on required roles and links to prerequisites and RBAC documentation (#7278)

## 0.0.4-preview (2026-03-17)

- Added async model registration with server-side validation and polling support
- Removed `--blob-uri` flag from `custom create` to prevent invalid data reference errors when registering models with externally uploaded blobs
- Improved 409 error handling in `custom create` with guidance to use `show` to fetch latest version
- `custom show` now defaults to latest version when `--version` is omitted
- `custom create` auto-extracts version from `--base-model` azureml:// URI when `--version` is not explicitly provided

## 0.0.3-preview (2026-02-19)

- Fixed azcopy download: added `githubusercontent.com` to allowed redirect hosts

## 0.0.2-preview (2026-02-19)

- Fixed azcopy download redirect to allow `github.com` as a trusted host

## 0.0.1-preview (2026-02-19)

- Initial release to support custom model creation
