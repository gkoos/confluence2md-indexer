# Embedding Providers

How `confluence2md-indexer` turns text into vectors, which providers it supports, and how to configure them.

For command mechanics, see [query-examples.md](query-examples.md) and [operations.md](operations.md).

## Concepts

### Provider

A provider converts text into vectors. Three ids are available (`--embedding list` prints them):

| id | mechanism | network |
| --- | --- | --- |
| `bow-local` (default) | local bag-of-words hashing | none |
| `openai` | OpenAI embeddings API | HTTPS |
| `openai-compatible` | any endpoint speaking the OpenAI embeddings protocol | HTTPS or localhost |

### Identity

Every provider reports a canonical **identity** string, which is the fingerprint of the vector space it produces:

```text
bow-local:fnv1a@256
openai:text-embedding-3-small@1536
openai-compatible:BAAI/bge-m3@1024+endpoint:cef65c52
openai-compatible:bge-m3@1024+endpoint:cef65c52+prefixes:5e6f7a8b
```

The identity is stored with every vector in the index, and command output reports it as `embedding identity` (`stats`) or `embedding.provider` (index JSON).

Only settings that change the vector space appear in it: model, dimension, endpoint and text prefixes. Operational settings such as batch size, timeout and retry count are deliberately excluded, so tuning them does not invalidate an existing index. Endpoints are stored as a short hash rather than in full, because gateway URLs sometimes carry credentials.

### Capabilities

Each provider reports a retrieval capability:

- `semantic` - a trained model, so similarity reflects meaning (`openai`, `openai-compatible`).
- `lexical` - term overlap only (`bow-local`). Useful and fully offline, but it cannot match synonyms.
- `none` - embeddings are disabled (`--skip-embeddings` or `CONFLUENCE2MD_EMBEDDING_SKIP=1`).

`stats` reports the capability of the vectors stored in an index, which tells you what kind of similarity the vector channel actually measures.

### Resolution order

Provider configuration is resolved once, in this order:

1. flags (`--embedding-*`), then
2. environment variables (`CONFLUENCE2MD_EMBEDDING_*`), then
3. the optional `embedding` section of the configuration file (`config.yaml` in the working directory, or a
   file named by `--config` / `CONFLUENCE2MD_CONFIG`), then
4. built-in defaults (`bow-local` at 256 dimensions).

Presence decides, not emptiness: a flag that was passed wins even when it sets an empty value, and an
environment variable counts when it is set to anything other than an empty string. The `embedding.source`
field in index output reports which layer chose the provider *id* (`flag`, `env`, `config` or `default`). The
file reference is in [operations.md](operations.md#configuration-file).

### The mismatch guard

Query commands compare the provider against the identities stored in the index before any vector math. If they differ, the query fails with exit code 2:

```text
query: embedding mismatch: index vectors are "bow-local:fnv1a@64" but the configured provider is
"bow-local:fnv1a@256"; re-index with --rebuild, select the stored provider, or query with --mode lexical
```

This exists because comparing vectors from different spaces yields zero similarity everywhere, which previously looked like `no results` with a successful exit code.

`--mode lexical` and `--lexical-only` need no provider at all, so they keep working even when provider configuration is missing or wrong.

## Offline default: `bow-local`

With no configuration at all, indexing embeds locally using a token-level hashing trick: tokens are lowercased, stopwords and single characters are dropped, remaining tokens are hashed into a fixed number of buckets with signed weights, and the vector is L2-normalized. Over-long runs (CJK text, long identifiers) are split into 3-grams.

```sh
confluence2md-indexer index ./output
confluence2md-indexer query --db ./output/confluence2md-index.db --q "how to rotate secrets"
```

Properties:

- No network, no API key, no cost, fully deterministic.
- Vector similarity here means *shared terms*, so it is correlated with BM25 rather than being a semantic signal.
- It is still worth having: FTS5 treats a multi-word query as a phrase, so a term-overlap vector channel can find hits the lexical channel misses.
- No stemming: `rotated` does not match `rotate`.
- Default dimension is 256 (about 1 KB per chunk); raise it with `--embedding-dim` to reduce hash collisions.

## OpenAI

```sh
export OPENAI_API_KEY=sk-...
confluence2md-indexer index ./output --embedding openai

# larger model, or a shortened vector
confluence2md-indexer index ./output --embedding openai --embedding-model text-embedding-3-large
confluence2md-indexer index ./output --embedding openai --embedding-dim 1024
```

- Default model is `text-embedding-3-small` (1536 dimensions).
- `--embedding-dim` only works for `text-embedding-3-*` models, which accept a `dimensions` parameter; other models reject it.
- The key is read from `OPENAI_API_KEY` unless you name another variable with `--embedding-api-key-env`.
- Requests are batched, retried with exponential backoff, and respect `Retry-After`.

## OpenAI-compatible endpoints

`openai-compatible` covers any endpoint with OpenAI's request and response shape. It requires a base URL, a model and a dimension, because nothing can be assumed about a third-party endpoint:

```sh
confluence2md-indexer index ./output \
  --embedding openai-compatible \
  --embedding-base-url https://api.siliconflow.cn/v1 \
  --embedding-model BAAI/bge-m3 \
  --embedding-dim 1024 \
  --embedding-api-key-env SILICONFLOW_API_KEY
```

`--embedding-dim` here **declares** the model's vector size: it is validated against every response, and it is not sent to the endpoint, because a given server may not accept a `dimensions` parameter. Truncating a model to fewer dimensions (Matryoshka) is therefore only available through the `openai` provider, whose v3 models document that parameter.

Hosted catalogs differ per site. SiliconFlow, for example, serves `BAAI/*` models on `api.siliconflow.cn` but only `Qwen/Qwen3-Embedding-*` on `api.siliconflow.com`, and keys are issued per site. List what your key can actually reach before choosing a model:

```sh
curl -sS <base-url>/models -H "Authorization: Bearer $SILICONFLOW_API_KEY" \
  | grep -oE '"id"[[:space:]]*:[[:space:]]*"[^"]+"' | sed -E 's/.*"([^"]+)"$/\1/' | sort
```

### Keyless local servers

Ollama, LM Studio, llama.cpp server, vLLM and LocalAI need no API key, and no auth header is sent when none is configured:

```sh
ollama serve
ollama pull bge-m3

confluence2md-indexer index ./output \
  --embedding openai-compatible \
  --embedding-base-url http://127.0.0.1:11434/v1 \
  --embedding-model bge-m3 \
  --embedding-dim 1024
```

LM Studio is the same with `http://127.0.0.1:1234/v1`.

### Azure OpenAI

Azure needs a deployment-scoped URL, a raw key in the `api-key` header, and an API version query parameter:

```sh
confluence2md-indexer index ./output \
  --embedding openai-compatible \
  --embedding-base-url https://<resource>.openai.azure.com/openai/deployments/<deployment> \
  --embedding-model text-embedding-3-small \
  --embedding-dim 1536 \
  --embedding-api-key-env AZURE_OPENAI_KEY \
  --embedding-auth-header api-key \
  --embedding-auth-scheme none \
  --embedding-query-param api-version=2024-02-01
```

`--embedding-auth-scheme none` sends the key without a scheme, that is, without a `Bearer ` prefix.

### Prefix-style models

Models such as `nomic-embed-text`, E5 and some bge variants expect different text for documents and queries:

```sh
--embedding-document-prefix "search_document: " --embedding-query-prefix "search_query: "
```

Prefixes are part of the identity, so index and query must be given the same values.

### Not covered

A server with a different wire shape needs its own adapter. HF text-embeddings-inference, for example, answers `/embed` (not `/v1/embeddings`) with a bare JSON array instead of an object containing a `data` field.

## Flag and environment reference

Every flag has an environment variable twin, `CONFLUENCE2MD_EMBEDDING_<NAME>`, and flags win over the environment. The configuration file (when present) writes the same settings as `embedding.<name>`, so `--embedding-base-url` is `CONFLUENCE2MD_EMBEDDING_BASE_URL` is `embedding.base_url`.

| flag | env suffix | meaning |
| --- | --- | --- |
| `--embedding` | `PROVIDER` | provider id, or `list` to enumerate providers |
| `--embedding-model` | `MODEL` | model name |
| `--embedding-base-url` | `BASE_URL` | base URL for HTTP providers |
| `--embedding-path` | `PATH` | path appended to the base URL (default `/embeddings`) |
| `--embedding-dim` | `DIM` | vector size; required for `openai-compatible`, and sent as a request parameter only by the `openai` provider |
| `--embedding-api-key-env` | `API_KEY_ENV` | name of the variable holding the API key |
| `--embedding-auth-header` | `AUTH_HEADER` | auth header name (default `Authorization`) |
| `--embedding-auth-scheme` | `AUTH_SCHEME` | auth scheme (default `Bearer`; `none` sends a raw key) |
| `--embedding-header k=v` | `HEADERS` | extra request headers, `;` separated |
| `--embedding-query-param k=v` | `QUERY_PARAMS` | extra query parameters, `;` separated |
| `--embedding-document-prefix` | `DOCUMENT_PREFIX` | text prefix for indexed text |
| `--embedding-query-prefix` | `QUERY_PREFIX` | text prefix for query text |
| `--embedding-batch-size` | `BATCH_SIZE` | inputs per request (default 64, capped per provider) |
| `--embedding-timeout` | `TIMEOUT` | per-request timeout (default 45s) |
| `--embedding-max-retries` | `MAX_RETRIES` | retries after the first attempt (default 3; `-1` disables) |
| `--skip-embeddings` (index only) | `SKIP` | store no vectors at all |
| `--lexical-only` (query only) | - | force `--mode lexical` |

Malformed values are reported rather than ignored: `CONFLUENCE2MD_EMBEDDING_DIM=wide` fails with `must be an integer`.

A credential has no flag, deliberately, because a key on the command line would land in shell history. Name a
variable with `--embedding-api-key-env` / `embedding.api_key_env`, or store the literal key as
`embedding.api_key` in the git-ignored `config.yaml`.

Setting the environment once is the most reliable way to keep index and query aligned:

```sh
export CONFLUENCE2MD_EMBEDDING_PROVIDER=openai-compatible
export CONFLUENCE2MD_EMBEDDING_BASE_URL=http://127.0.0.1:11434/v1
export CONFLUENCE2MD_EMBEDDING_MODEL=bge-m3
export CONFLUENCE2MD_EMBEDDING_DIM=1024
```

## Choosing a provider

| situation | choice |
| --- | --- |
| offline, no key, first look at a corpus | `bow-local` (default) |
| exact terminology, configuration keys, service names | `--mode lexical`, with any provider |
| synonyms and paraphrase matter | `openai`, or an `openai-compatible` model |
| content must not leave the machine, semantic quality wanted | `openai-compatible` against Ollama or LM Studio |
| token cost matters | a local model, or shortened vectors via `--embedding-dim` on `text-embedding-3-*` |

## Switching providers

Re-run indexing with the new settings. No `--rebuild` is needed: a document is skipped only when both its content and the stored identity match, so a provider change re-embeds the corpus and the previous identity's vectors are removed.

```sh
confluence2md-indexer index ./output --embedding openai
confluence2md-indexer index ./output            # back to the offline default, re-embedded
```

`--rebuild` is only needed to start from an empty database, for example for an index written by an older build whose schema predates identity tracking.

Check what an index actually holds:

```sh
confluence2md-indexer stats --db ./output/confluence2md-index.db
confluence2md-indexer stats --db ./output/confluence2md-index.db --json
```

`stats` reports `embeddings`, `vector ready`, `embedding identity` and `vector capability`, plus per-identity counts in JSON.

## Index size

Vectors cost `dimension * 4` bytes per chunk before SQLite overhead:

| dimension | per chunk | 25k chunks |
| --- | --- | --- |
| 256 (`bow-local` default) | ~1 KB | ~25 MB |
| 1024 (`bge-m3`) | ~4 KB | ~100 MB |
| 1536 (`text-embedding-3-small`) | ~6 KB | ~150 MB |

Chunk text and the FTS index usually dominate total database size for text-heavy corpora.

## Troubleshooting

| symptom | cause and fix |
| --- | --- |
| `embedding mismatch: index vectors are "A" but the configured provider is "B"` | index and query settings differ; pass the same flags, or use `--lexical-only` |
| `the index holds no embeddings to search with "..."` | the index was built with `--skip-embeddings`; re-index without it |
| `unknown embedding provider "x" (available: ...)` | typo in `--embedding`; run `index --embedding list` |
| `openai-compatible requires --embedding-dim` | an arbitrary model has no locally known dimension |
| `openai requires an API key in OPENAI_API_KEY` | set the key, or point `--embedding-api-key-env` at the variable holding it |
| `embeddings response dimension N does not match the configured dimension M` | the model's real dimension differs from `--embedding-dim` |
| vector results look arbitrary | the index holds `bow-local` vectors, which measure term overlap only; check `vector capability` in `stats` |
| `vector ready: false` after indexing | embeddings were skipped, or the provider failed before writing any vector |

## Adding a provider

`internal/embedding` is built for this:

1. Implement `embedding.Provider` (`Name`, `Dimension`, `Caps`, `Embed`) in a new file.
2. Register it with `MustRegister(<id>, <factory>)` so `--embedding <id>` and `--embedding list` pick it up.
3. Make `Name()` include everything that changes the vector space, and nothing else.
4. Set `Caps.Semantic` or `Caps.Lexical`, plus `Caps.Asymmetric` when queries and documents are transformed differently.
5. Add the provider to the conformance suite in `internal/embedding/conformance_test.go`; every adapter is expected to pass it.
