package embedding

const (
	// DefaultMaxLength is the default maximum sequence length for tokenization.
	DefaultMaxLength = 512
)

// Tokenizer converts text to token IDs suitable for model input.
type Tokenizer interface {
	// Tokenize converts text to a slice of token IDs.
	// The result is padded or truncated to MaxLength().
	Tokenize(text string) ([]int32, error)

	// Decode converts a slice of token IDs back to the original text.
	Decode(tokens []int32) (string, error)

	// MaxLength returns the maximum sequence length for padding/truncation.
	MaxLength() int
}
