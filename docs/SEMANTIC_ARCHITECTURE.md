# Semantic architecture

GoGIF now reads a prompt once, in one place, and lets every downstream system
use that reading. This document describes the layers, how to configure each
one, and what is genuinely possible with hosted and quantized models.

## The four layers

| Layer | Package | Before | Now |
| --- | --- | --- | --- |
| Interpretation | `internal/intent` | none — the local planner hashed the prompt to pick a palette | a deterministic offline reading of subject, action, setting, camera, style, mood, negative prompt, and search keywords |
| Planning | `internal/planner` | `{caption, palette[5], motion}` | the same brief, produced offline, by OpenAI, or by any OpenAI-compatible chat endpoint |
| Generation | `internal/imagegen` | a static prompt template around the raw sentence | scene sentence, art direction, and negative prompt built from the brief |
| Discovery | `internal/provider`, `internal/semantic`, `internal/subtitle` | keyword pass-through and token-distance quote matching | sentence queries distilled to catalog terms, results reranked by meaning, paraphrased quotes located in transcripts |

The brief is the contract. `intent.Brief` uses closed vocabularies for camera,
style, and mood, so a model that invents a value fails validation and the
request falls back to the offline reading rather than rendering something the
user did not ask for.

### Interpretation is free and always available

`intent.Interpret` is lexicon and syntax driven: no model, no network, no key.
It finds the action verb, splits the setting at its preposition, strips request
filler ("please make me a gif of…") from the subject, and maps style, mood, and
camera words onto the closed vocabularies. It is the fallback for every remote
interpreter and the default planner for a zero-spend deployment.

## Planning

```sh
GOGIF_PLANNER=local          # offline reading only
GOGIF_PLANNER=openai         # Responses API, strict structured output
GOGIF_PLANNER=huggingface    # any OpenAI-compatible chat completions endpoint
```

The Hugging Face planner talks to `POST {base}/chat/completions` with a strict
`json_schema` response format. Two deployments share it because they speak the
same protocol:

```sh
# Hosted open-weights models through the Inference Providers router.
# Billed at the provider's own rate, so it requires the paid-AI opt-in.
GOGIF_ENABLE_PAID_AI=true
HUGGINGFACE_API_KEY=hf_...
GOGIF_HUGGINGFACE_MODEL=openai/gpt-oss-120b:cheapest
GOGIF_HUGGINGFACE_BASE_URL=https://router.huggingface.co/v1
```

```sh
# A local quantized model. No token, no opt-in, no bill.
GOGIF_PLANNER=huggingface
GOGIF_HUGGINGFACE_BASE_URL=http://127.0.0.1:8080/v1
GOGIF_HUGGINGFACE_MODEL=local-model
```

Non-loopback planning hosts are allowlisted, because the prompt is user text.

## Discovery

Two problems, two fixes, both fail open.

**Sentence queries.** Archive catalogs index concrete nouns. `provider.Interpreted`
distills a query of six or more words down to its interpreted terms before it
reaches a provider, and forwards anything shorter exactly as typed. Quotes are
never distilled.

**Keyword relevance.** `semantic.Ranker` reorders a result page by cosine
similarity between the user's original words and each result's title, quote,
description, and author. Rights metadata and URLs are excluded so licence
boilerplate cannot dominate similarity. The semantic score is blended with the
provider's own ordering (`GOGIF_EMBEDDING_WEIGHT`, default `0.6`) so a marginal
vector difference cannot displace a confident upstream match.

```sh
GOGIF_EMBEDDING_PROVIDER=lexical        # offline default
GOGIF_EMBEDDING_PROVIDER=huggingface    # hosted CPU embeddings
GOGIF_EMBEDDING_MODEL=BAAI/bge-small-en-v1.5
GOGIF_EMBEDDING_URL=https://router.huggingface.co
```

`semantic.Lexical` is the offline embedder: hashed stemmed tokens plus
character 4-grams. It relates "running" to "runs" but not "puppy" to "dog".
It exists so ranking degrades instead of disappearing, and so the ranking path
stays testable with no network.

Embeddings are memoized by `semantic.Cached`, because paging a search re-shows
the same titles and a hosted embedder would otherwise be billed for identical
inputs on every scroll.

`subtitle.FindSemantic` locates a paraphrased quote in a transcript. Exact and
fuzzy token matching still run first; only when both fail does it embed
three-cue windows and take the best match above `0.55`. The result is never
marked exact, and its confidence is the similarity score.

## Quantized models

Yes to all three questions: hosted quantized models, self-hosted quantized
models, and quantizing models ourselves. Nothing in the architecture prevents
any of them; the constraints are hardware and licence, not code.

### Running a quantized model locally

The `flux-gguf` recipe loads a quantized transformer through the
[ComfyUI-GGUF](https://github.com/city96/ComfyUI-GGUF) custom nodes —
`UnetLoaderGGUF` with `DualCLIPLoader` and `VAELoader` instead of a single
checkpoint file. That separation is what makes a FLUX-class model fit a small
GPU.

```sh
GOGIF_IMAGE_GENERATOR=comfyui
GOGIF_COMFYUI_RECIPE=flux-gguf
GOGIF_COMFYUI_GGUF_UNET=flux1-schnell-Q4_K_S.gguf
GOGIF_COMFYUI_GGUF_CLIP_L=clip_l.safetensors
GOGIF_COMFYUI_GGUF_CLIP_T5=t5xxl_fp8_e4m3fn.safetensors
GOGIF_COMFYUI_GGUF_VAE=ae.safetensors
GOGIF_COMFYUI_STEPS=4
```

The recipe has no image-to-image path. FLUX conditioning for references works
differently, and silently discarding a licensed reference would be worse than
refusing it, so the adapter rejects requests that carry one.

### Which machine

| Host | Quantized LLM (planning) | Quantized embeddings | Quantized diffusion |
| --- | --- | --- | --- |
| 8 GB M3 Mac | yes — a 3–4B model at Q4 is about 2–3 GB | yes — 100–400 MB | no — Q4 FLUX needs roughly 7 GB before the OS, the app, and the browser |
| RTX 4060 Ti worker | yes | yes | yes — Q4_K_S is about 6.8 GB and is the documented sweet spot for an 8 GB card |

This is why the ComfyUI adapter now accepts a private endpoint. The loopback
restriction was correct for an SSRF boundary but meant the Mac could not use
the one machine in the building with a real GPU:

```sh
GOGIF_COMFYUI_PRIVATE_ENDPOINT=true
GOGIF_COMFYUI_URL=http://gpu-box.tail1234.ts.net:8188
GOGIF_COMFYUI_AUTH_TOKEN=...
```

The host must be RFC1918, `100.64.0.0/10` (Tailscale), or a `.ts.net` /
`.internal` / `.local` / `.lan` name. That is not a security boundary by
itself — the token and the tailnet are — but it stops a typo from sending
prompts to a public address. The opt-in exists because prompts leave this
machine when it is on.

### Hosting quantized models in the cloud

Three routes, in increasing order of control:

1. **Inference Providers router.** Pay per token or per image at the
   provider's own rate; the provider chooses the precision. No control over
   quantization, no idle cost.
2. **Dedicated inference endpoints.** Deploy a specific quantized artifact —
   GGUF, AWQ, GPTQ — on hardware that stays warm. Full control over the
   weights, billed by the hour whether or not anything is generated.
3. **A container of our own** running ComfyUI plus ComfyUI-GGUF, or
   llama.cpp's server, on any GPU host. The `flux-gguf` recipe and the
   loopback planner path already speak to exactly this; a cloud GPU with a
   private address is the same configuration as the 4060 Ti.

### Quantizing models ourselves

This is ordinary tooling, not research:

- **LLMs** — `llama.cpp`'s `convert_hf_to_gguf.py` then `llama-quantize` to
  Q4_K_M or Q5_K_M. The result serves over llama.cpp's OpenAI-compatible
  endpoint, which the Hugging Face planner already drives via a loopback base
  URL.
- **Diffusion transformers** — ComfyUI-GGUF ships a conversion script for
  turning a FLUX-class transformer into GGUF at a chosen precision. Useful for
  a fine-tune or a model nobody has published a quantization of.
- **Embedding models** — ONNX Runtime dynamic int8 quantization takes a
  `bge-small`-class encoder to roughly 30 MB with minor recall loss. This is
  the most interesting one for GoGIF: it is small enough to serve beside the Go
  process on any machine, including the Mac, which would make semantic search
  fully offline and zero-cost.

Higher precision is the safer default for final output: Q8 for production
imagery, Q4 for prototyping and for hardware that cannot hold anything larger.

### Licences do not quantize away

A quantization inherits the base model's licence.

| Model | Licence |
| --- | --- |
| FLUX.1-schnell | Apache-2.0 |
| FLUX.1-dev | non-commercial |
| Stable Diffusion 1.5 | CreativeML Open RAIL-M |

Nothing in the process can determine the licence of a local file, so read the
model card before shipping output made with it.

## What is deliberately not built

- No vector database. Ranking is brute-force cosine over one result page,
  which is the right size for this problem and adds no infrastructure.
- No embedding of user library contents. Only the query and public catalog
  metadata are embedded; no user media and no account identifiers leave the
  process.
- No semantic rewriting of quotes. A quote is matched verbatim first, and a
  paraphrase match is always reported as one.
