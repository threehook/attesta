# Authorization Decision Log

Attesta writes one record per decision, as an [ADL 1.0](https://gitdocumentatie.logius.nl/publicatie/ftv/adl/1.0.0/) Level 1 record. A decision is
the answer to a wallet's response: the sign-in and every request, allowed or denied.

## What is recorded

| Situation | `status` | `adl.core.response` |
|---|---|---|
| A policy decided (allow or deny) | `Ok` | `decision` and `context.reason.policy` |
| The credential did not verify | `Ok` | `decision: false`, `context.reason.attesta` = "credential could not be verified", `context.reason.detail` = why |
| The credential was revoked | `Ok` | `decision: false`, `context.reason.attesta` = "credential has been revoked", `context.reason.detail` = which credential |
| The policy could not be evaluated (unknown policy, a policy error) | `Error` | absent; the cause is in `attributes.error` |

A wallet answering an unknown, expired or already answered request is not a decision and is not recorded.

The request is written in the AuthZEN shape the standard prescribes (Attesta itself does not use AuthZEN): `subject` is the holder (`id` is the identity claim, the email unless the request named another,
`properties.issuer` the issuer DID; `unknown` when the credential did not verify), `action.name` is always `authorize`, `resource.id` is the resource
the application asked about, and `context` holds the request id, policy id, credential type, the disclosed claims the policy received, `also` (the type, issuer and claims of each further credential, when a request asked for several) and the
`user_roles` the application sent, marked `origin: application` because Attesta cannot verify them. The `action` the application described (a few texts by name) is kept as `action_details`, marked the same way. There are no source references (`attributes` is
empty): those start at Level 2.

## Trace

The application sends a W3C `traceparent` when it starts a request. Attesta keeps it with the request, because the wallet's answer arrives later
without one. The record carries that `trace_id`, the application's span as `parent_span_id`, and a fresh `span_id`. Without a (valid) `traceparent`
a new trace starts. The sampled flag is not used: every decision is recorded.

## Configuration

| Variable | Meaning |
|---|---|
| `ATTESTA_ADL_OUTPUT` | Any of `stdout`, `file`, `otlp`, comma-separated. Default `stdout`. |
| `ATTESTA_ADL_FILE` | Base path of the record files for `file`. Attesta does not start when they cannot be opened. |
| `ATTESTA_ADL_OTLP_ENDPOINT` | OTLP/gRPC collector, such as `alloy.observability.svc.cluster.local:4317`. |
| `ATTESTA_ADL_OTLP_INSECURE` | `true` sends without TLS. |
| `ATTESTA_ADL_RESOURCE` | Producer identity, `key=value,key=value`. `service.name` defaults to `attesta-adl`, `instance_id` to the pod name. |

stdout writes the record as one JSON line. otlp sends the same JSON as the OTLP log body, with `trace_id` and `span_id` as native fields and the
resource as OTLP resource attributes. file appends the same JSON line to the record file and fsyncs it. The record file is a series of segments, `ATTESTA_ADL_FILE` + `.000001`,
`.000002` and so on; a new segment starts at 64 MiB. Keep them on a persistent volume.

With file and otlp together, the OTLP export reads the segments in order: it sends the records after the position saved in `ATTESTA_ADL_FILE` +
`.offset` (segment and offset) and moves it once the collector accepted them. After a collector outage or a restart it sends what was missed, so a
record can arrive twice. A segment that is sent in full is kept for one day from then, in case the collector lost what it accepted, and then deleted;
the segment being written and segments not yet sent are never deleted. Without file, otlp sends from memory in batches and a collector outage loses
what is still queued.

A record is written before the decision is handed to the application. If the file or stdout record cannot be written, no decision is returned:
the application gets a denial ("decision could not be logged") and the wallet a server error.

## Observing it

Alloy receives the records on OTLP and writes them to Loki; Grafana has Loki as a data source. Alloy also reads the pod logs of the `attesta`
namespace and drops the ADL lines there, so a decision is stored once. In Grafana's Explore:

```logql
{service_name="attesta-adl"}                                                          # every decision
{service_name="attesta-adl"} | json | body_adl_core_response_decision="false"         # denials
{service_name="attesta-adl"} | json | status="Error"                                  # Attesta could not evaluate
{service_name="attesta-adl"} | trace_id="28dbeec32e77635cc19bc3204ec56c41"            # one flow
{service_name="attesta-adl"} | application="laadpalen"                                # one application's sidecar
{service_name="attesta"}                                                              # Attesta's regular log lines
```

## Not met

- TLS to the log: the collector is reached inside the cluster without it.
- Once a sent segment is deleted, Loki holds the only copy of its records; nothing replays from a deleted segment.
- Idempotent ingestion: Loki drops identical lines in a stream, which is not a guarantee.
- Trace context is kept and recorded, but Attesta makes no outgoing calls to pass it on.
- Retention follows Loki's (31 days here); the organisation's own retention policy is not applied.
- Subject ids and claim values are logged as they are, not pseudonymised.
