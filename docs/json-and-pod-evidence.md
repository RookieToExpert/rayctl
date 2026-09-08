# JSON output and Pod evidence

Read commands accept `-o table|json` (table is the default). Metrics retains
its existing JSON schema and CSV support; node health retains its existing
JSON schema. Aliases use the same implementation as their canonical command.

Examples:

```sh
rayctl aid list -n 5 -o json
rayctl aid get dev-example -l -o json
rayctl job get job-example -l --no-image -o json
rayctl air job get infer-example -l -o json
rayctl node describe host-example -o json
rayctl queue list -o json
rayctl auth check user example -o json
```

## Output contract

- A single detail query returns an object with `warnings` and `errors`.
- Lists and multi-target queries return `items`, `summary`, `metadata`,
  `warnings`, and `errors`. `summary.returned` is the number of returned
  objects; `summary.total` is null when the source did not supply a total.
  `summary.complete` indicates whether the command reported a query failure.
- Failed targets do not discard successful results. A query error gives a
  nonzero exit code; stdout still contains one JSON document. Usage and
  diagnostic messages go to stderr. Invalid arguments may fail before querying.
- JSON is projected from query results before table formatting. Names, UIDs,
  messages, and other fields are not shortened for terminal width. List limits
  remain effective; JSON does not implicitly request all pages or enable `-l`.
- Fields use snake_case. Unavailable scalar fields are null with warnings.
  Legacy sources that do not distinguish missing from inapplicable report
  that uncertainty instead of inventing a cause.
- Recognized resource quantities have `value`, `unit`, and `raw`, preserving
  the source scale. A source quantity lacking a reliable unit has `unit: null`.
  Resource summaries remain strings alongside the structured detail fields.
- Timestamps are RFC3339 with UTC+8. Query durations declare nanoseconds.
- Internal `Raw` platform payloads are excluded. Credential fields and known
  credential forms in text are redacted. Application logs may contain other
  sensitive business data; the JSON is not a copy of Kubernetes Secrets.
- Optional expensive data is still collected only with the existing flags.
  Omitted log collection is distinct from an empty log response.

## Pod evidence

`aid get -l`, `ait get -l` / `job get -l`, and `air job get -l` include
`pods[]` with full container evidence and `pod_evidence` with selection/history
metadata. Existing lightweight Pod summaries are retained as `pod_summary`.
Without evidence collection, `pods` is null. AIR gateway JSON is supported, but gateway Pod evidence is
outside this three-workload scope.

- Full current Pod statuses are ranked before selecting three Pods. Pending,
  failed or unknown Pods rank first, followed by unready containers, highest
  container restart count, then earliest available last termination time.
  Successful completed containers do not count as unready failures.
- Every selected Pod includes init and application containers, current state,
  last termination, exit code, and lifetime restart count. `instant_exit` is
  only defined when both timestamps exist and the nonnegative duration is
  less than one second. This is a timing observation, not a cause diagnosis.
- Current logs use tail 30. Previous logs use tail 30 only when the container
  restart count is positive. Each stream has a 1 MiB safety limit and explicit
  truncation flags. Missing logs remain null with warnings.
- Three Pods are collected concurrently. Individual log, node, and image
  calls have a three-second timeout; the evidence collection budget is ten
  seconds, also bounded by the command context.
- Images are compared per container. Harbor artifact metadata uses matching
  imagePullSecret credentials and the actual image digest when available.
  Equal references share one request within a collection. TLS verification
  remains enabled and authenticated redirects are not followed. A missing
  architecture, including an unresolved multi-platform index, yields null
  comparison instead of a mismatch. `--no-image` skips Harbor queries.
- `restarts` counts all current Pod container counters. `restarts_24h` is the
  observed metric increment for exactly `history_pods`: all discovered AID
  Pods, or the three selected AIT/AIR Pods. It is null if the history query
  fails or returns no samples. History uses the existing metrics restart
  implementation and an additional three-second budget.
- AID readiness text reports current readiness and historical restarts.
  It never infers stability from readiness alone. `aid list` adds RESTARTS
  via one batched Pod query, not one request per AID. Deleted Pods cannot
  contribute to the current Pod counter total.

## Verification

Regression tests cover parseable single-document output, partial query errors,
secret redaction, intact long fields, timezone and resource units, invalid
termination timestamps, completed init containers, multi-container evidence,
selection beyond the first twenty Pods, and Harbor repository URL encoding.

Live fixture state changes over time. The September 4 sample restart counts,
architecture mismatch, and previous log lines must not be treated as permanent
expected values for the September 7 deployment.
