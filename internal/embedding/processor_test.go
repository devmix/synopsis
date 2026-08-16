package embedding_test

import (
	"testing"

	"github.com/devmix/synopsis/internal/embedding"
)

func TestPreprocess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		maxRuneLimit int
		want         string
	}{
		{
			name: "empty string",
			input: "",
			maxRuneLimit: 100,
			want: "",
		},
		{
			name: "collapse multiple spaces",
			input: "hello    world   foo",
			maxRuneLimit: 100,
			want: "hello world foo",
		},
		{
			name: "trim leading and trailing whitespace",
			input: "  hello world  ",
			maxRuneLimit: 100,
			want: "hello world",
		},
		{
			name: "remove control characters",
			input: "hello\x00world\x01foo",
			maxRuneLimit: 100,
			want: "helloworldfoo",
		},
		{
			name: "truncate long text",
			input: "abcdefghij",
			maxRuneLimit: 5,
			want: "abcde",
		},
		{
			name: "no truncation when within limit",
			input: "short",
			maxRuneLimit: 100,
			want: "short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := embedding.Preprocess(tt.input, tt.maxRuneLimit)
			if got != tt.want {
				t.Errorf("Preprocess() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPreprocessBatch(t *testing.T) {
	t.Parallel()

	inputs := []string{"  hello   world  ", "foo\tbar"}
	got := embedding.PreprocessBatch(inputs, 100)

	want := []string{"hello world", "foo bar"} // tab is normalized to space
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplitBatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     []string
		batchSize int
		wantLen   int
	}{
		{
			name:      "single batch",
			input:     []string{"a", "b"},
			batchSize: 10,
			wantLen:   1,
		},
		{
			name:      "two batches",
			input:     []string{"a", "b", "c", "d"},
			batchSize: 2,
			wantLen:   2,
		},
		{
			name:      "empty input",
			input:     nil,
			batchSize: 5,
			wantLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := embedding.SplitBatches(tt.input, tt.batchSize)
			if len(got) != tt.wantLen {
				t.Errorf("SplitBatches() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}
