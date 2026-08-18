package llama

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// statusLoading is the state reported while a model is still being loaded.
const statusLoading = "loading model"

// Health is the /health response.
type Health struct {
	Status string `json:"status"`
}

// OK reports whether the server declared itself ready.
func (h Health) OK() bool { return strings.EqualFold(h.Status, "ok") }

// Loading reports whether the server is up but still loading a model.
func (h Health) Loading() bool { return strings.EqualFold(h.Status, statusLoading) }

// Props is the subset of /props that ltop displays. llama.cpp returns a large
// document including the full chat template; unused fields are dropped.
type Props struct {
	ModelPath       string     `json:"model_path"`
	BuildInfo       string     `json:"build_info"`
	IsSleeping      bool       `json:"is_sleeping"`
	EndpointSlots   bool       `json:"endpoint_slots"`
	EndpointMetrics bool       `json:"endpoint_metrics"`
	TotalSlots      int        `json:"total_slots"`
	Modalities      Modalities `json:"modalities"`
}

// Modalities reports the input types the loaded model accepts.
type Modalities struct {
	Vision bool `json:"vision"`
	Video  bool `json:"video"`
	Audio  bool `json:"audio"`
}

// ModelName is the GGUF filename without directory or extension.
func (p Props) ModelName() string {
	if p.ModelPath == "" {
		return ""
	}
	base := filepath.Base(p.ModelPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Slot is one entry of the /slots array: a server-side generation seat.
type Slot struct {
	ID           int  `json:"id"`
	NCtx         int  `json:"n_ctx"`
	Speculative  bool `json:"speculative"`
	IsProcessing bool `json:"is_processing"`
	TaskID       int  `json:"id_task"`

	// PromptTokens is the full prompt length of the current or last task.
	PromptTokens int `json:"n_prompt_tokens"`
	// PromptProcessed counts tokens this slot had to run through prefill.
	PromptProcessed int `json:"n_prompt_tokens_processed"`
	// PromptCached counts tokens served from the KV cache instead of prefill.
	PromptCached int `json:"n_prompt_tokens_cache"`
}

// ContextUsed is the fraction of the slot's context window occupied, in 0..1.
func (s Slot) ContextUsed() float64 {
	if s.NCtx <= 0 {
		return 0
	}
	used := float64(s.PromptTokens) / float64(s.NCtx)
	if used > 1 {
		return 1
	}
	return used
}

// Model is one entry of the /v1/models data array.
type Model struct {
	ID   string    `json:"id"`
	Meta ModelMeta `json:"meta"`
}

// ModelMeta carries the GGUF header values llama.cpp exposes per model.
type ModelMeta struct {
	NVocab    int    `json:"vocab_type"`
	NCtx      int    `json:"n_ctx"`
	NCtxTrain int    `json:"n_ctx_train"`
	NEmbd     int    `json:"n_embd"`
	NParams   int64  `json:"n_params"`
	Size      int64  `json:"size"`
	FType     string `json:"ftype"`
}

type modelsResponse struct {
	Data []Model `json:"data"`
}

func decodeModels(b []byte) ([]Model, error) {
	var r modelsResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return r.Data, nil
}

// MatchModel finds the /v1/models entry describing the model named in props.
//
// A router such as llama-swap serves several models from one endpoint, so
// taking the first entry would pair one model's name with another's size and
// quantisation. When no entry matches confidently the caller gets no metadata
// rather than wrong metadata.
func MatchModel(models []Model, props Props) (Model, bool) {
	switch len(models) {
	case 0:
		return Model{}, false
	case 1:
		return models[0], true
	}

	name := strings.ToLower(props.ModelName())
	if name == "" {
		return Model{}, false
	}

	for _, m := range models {
		if strings.EqualFold(m.ID, props.ModelName()) {
			return m, true
		}
	}
	// llama.cpp usually serves an alias shorter than the GGUF filename, for
	// example id qwen3.8-27b for Qwen3.8-27B-UD-Q4_K_XL.gguf.
	for _, m := range models {
		id := strings.ToLower(m.ID)
		if id != "" && (strings.Contains(name, id) || strings.Contains(id, name)) {
			return m, true
		}
	}
	return Model{}, false
}
