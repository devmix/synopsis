package embedding

import (
	"testing"

	"github.com/yalue/onnxruntime_go"
)

func graphOf(names ...string) []onnxruntime_go.InputOutputInfo {
	info := make([]onnxruntime_go.InputOutputInfo, len(names))
	for i, n := range names {
		info[i] = onnxruntime_go.InputOutputInfo{Name: n}
	}
	return info
}

func TestInputNamesFor(t *testing.T) {
	tests := []struct {
		name    string
		names   []string
		want    []string
		wantErr bool
	}{
		{
			name:  "bge-small style with token_type_ids",
			names: []string{"input_ids", "attention_mask", "token_type_ids"},
			want:  []string{"input_ids", "attention_mask", "token_type_ids"},
		},
		{
			name:  "bge-m3 style without token_type_ids",
			names: []string{"input_ids", "attention_mask"},
			want:  []string{"input_ids", "attention_mask"},
		},
		{
			name:  "canonical order restored from shuffled graph",
			names: []string{"token_type_ids", "attention_mask", "input_ids"},
			want:  []string{"input_ids", "attention_mask", "token_type_ids"},
		},
		{
			name:    "missing input_ids",
			names:   []string{"attention_mask", "token_type_ids"},
			wantErr: true,
		},
		{
			name:    "missing attention_mask",
			names:   []string{"input_ids"},
			wantErr: true,
		},
		{
			name:    "empty graph",
			names:   nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inputNamesFor(graphOf(tt.names...))
			if (err != nil) != tt.wantErr {
				t.Fatalf("inputNamesFor() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("inputNamesFor() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSelectOutputName(t *testing.T) {
	tests := []struct {
		name    string
		names   []string
		want    string
		wantErr bool
	}{
		{
			name:  "prefers pooled sentence_embedding",
			names: []string{"token_embeddings", "sentence_embedding"},
			want:  "sentence_embedding",
		},
		{
			name:  "last_hidden_state fallback",
			names: []string{"last_hidden_state"},
			want:  "last_hidden_state",
		},
		{
			name:  "single arbitrary output accepted",
			names: []string{"hidden_states"},
			want:  "hidden_states",
		},
		{
			name:    "multiple outputs without supported name",
			names:   []string{"token_embeddings", "pooler_output"},
			wantErr: true,
		},
		{
			name:    "no outputs",
			names:   nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectOutputName(graphOf(tt.names...))
			if (err != nil) != tt.wantErr {
				t.Fatalf("selectOutputName() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("selectOutputName() = %q, want %q", got, tt.want)
			}
		})
	}
}
