package llama

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Health is the /health response.
type Health struct {
	Status string `json:"status"`
}

// OK reports whether the server declared itself ready.
func (h Health) OK() bool { return strings.EqualFold(h.Status, "ok") }

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
