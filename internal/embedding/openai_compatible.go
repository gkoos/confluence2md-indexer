package embedding

// newOpenAICompatibleProvider serves endpoints that speak the OpenAI embeddings
// wire protocol, which is how this tool reaches providers and local servers other
// than OpenAI itself.
//
// Verified request shapes:
//
//   - Azure OpenAI: point --embedding-base-url at the deployment, then use
//     --embedding-auth-header api-key --embedding-auth-scheme none plus
//     --embedding-query-param api-version=2024-02-01
//   - keyless local servers: Ollama (http://127.0.0.1:11434/v1), LM Studio
//     (http://127.0.0.1:1234/v1), llama.cpp server, vLLM, LocalAI
//   - OpenAI-compatible hosted APIs, for example SiliconFlow, DeepInfra, Mistral
//     or Jina, via --embedding-api-key-env
//
// Servers that expose a different shape need their own adapter rather than this
// one: HF text-embeddings-inference, for instance, answers /embed (not
// /v1/embeddings) with a bare JSON array instead of an object with a data field.
//
// The dimension of an arbitrary model is not known locally, so this provider
// requires --embedding-dim. There is no default model or base URL either, since
// nothing is assumed about the endpoint.
func newOpenAICompatibleProvider(opts Options) (Provider, error) {
	return newOpenAIWireProvider(opts, wireDefaults{
		id:          ProviderOpenAICompatible,
		defaultPath: openAIEmbedPath,
		// An arbitrary model's dimension cannot be looked up, so it must be stated.
		requireDimension: true,
		// Local servers are commonly keyless, so no key env var is assumed.
	})
}

func init() {
	MustRegister(ProviderOpenAICompatible, newOpenAICompatibleProvider)
}
