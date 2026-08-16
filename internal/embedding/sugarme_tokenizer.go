package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sugarme "github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/model/bpe"
	"github.com/sugarme/tokenizer/model/wordpiece"
	"github.com/sugarme/tokenizer/pretokenizer"
	"github.com/sugarme/tokenizer/pretrained"
)

// SugarmeTokenizer wraps github.com/sugarme/tokenizer and implements the Tokenizer interface.
// It supports BPE (vocab.json + merges.txt) and WordPiece (vocab.txt) tokenization models.
type SugarmeTokenizer struct {
	tokenizer *sugarme.Tokenizer
	maxLength int
	padID     int32
	unkToken  string
}

// NewSugarmeTokenizer creates a tokenizer by loading the model from the given path.
// The path can be:
//   - A directory containing tokenizer.json (HuggingFace format)
//   - A directory containing vocab.json and merges.txt (BPE model)
//   - A single file with one token per line (WordPiece model, e.g., vocab.txt)
//
// maxLength controls padding/truncation length; use DefaultMaxLength for typical models.
func NewSugarmeTokenizer(modelPath string, maxLength int) (*SugarmeTokenizer, error) {
	if modelPath == "" {
		return nil, fmt.Errorf("sugarme tokenizer: model path is required")
	}

	if maxLength <= 0 {
		maxLength = DefaultMaxLength
	}

	var (
		tz       *sugarme.Tokenizer
		padID    int32
		unkToken string
		err      error
	)

	info, statErr := os.Stat(modelPath)
	if statErr != nil {
		return nil, fmt.Errorf("sugarme tokenizer: stat %s: %w", modelPath, statErr)
	}

	if info.IsDir() {
		// Try HuggingFace tokenizer.json first
		tjPath := filepath.Join(modelPath, "tokenizer.json")
		if _, statErr := os.Stat(tjPath); statErr == nil {
			tz, err = loadTokenizerJSON(tjPath, maxLength)
			if err != nil {
				return nil, fmt.Errorf("sugarme tokenizer: %w", err)
			}
		} else {
			tz, padID, unkToken, err = loadBPEFromDir(modelPath)
			if err != nil {
				return nil, fmt.Errorf("sugarme tokenizer: %w", err)
			}
		}
	} else {
		// Check if it's a tokenizer.json file directly
		if strings.HasSuffix(modelPath, "tokenizer.json") {
			tz, err = loadTokenizerJSON(modelPath, maxLength)
			if err != nil {
				return nil, fmt.Errorf("sugarme tokenizer: %w", err)
			}
		} else {
			tz, padID, unkToken, err = loadWordPieceFromFile(modelPath)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("sugarme tokenizer: %w", err)
	}

	return &SugarmeTokenizer{
		tokenizer: tz,
		maxLength: maxLength,
		padID:     padID,
		unkToken:  unkToken,
	}, nil
}

// loadBPEFromDir loads a BPE tokenizer from a directory containing vocab.json and merges.txt.
func loadBPEFromDir(dir string) (*sugarme.Tokenizer, int32, string, error) {
	vocabFile := filepath.Join(dir, "vocab.json")
	mergesFile := filepath.Join(dir, "merges.txt")

	if _, err := os.Stat(vocabFile); os.IsNotExist(err) {
		return nil, 0, "", fmt.Errorf("BPE vocab file not found: %s", vocabFile)
	}
	if _, err := os.Stat(mergesFile); os.IsNotExist(err) {
		return nil, 0, "", fmt.Errorf("BPE merges file not found: %s", mergesFile)
	}

	model, err := bpe.NewBpeFromFiles(vocabFile, mergesFile)
	if err != nil {
		return nil, 0, "", fmt.Errorf("load BPE model: %w", err)
	}

	tz := sugarme.NewTokenizer(model)

	// Add ByteLevel pre-tokenizer for BPE models.
	bpePretok := pretokenizer.NewByteLevel()
	tz.WithPreTokenizer(bpePretok)

	padID := int32(0) // Default pad Predicate for BPE models.
	unkTokenPtr := model.GetUnkToken()
	var unkToken string
	if unkTokenPtr != nil {
		unkToken = *unkTokenPtr
	} else {
		unkToken = "[UNK]"
	}

	return tz, padID, unkToken, nil
}

// loadWordPieceFromFile loads a WordPiece tokenizer from a vocab file (one token per line).
func loadWordPieceFromFile(vocabFile string) (*sugarme.Tokenizer, int32, string, error) {
	padID := int32(0) // Default pad Predicate for WordPiece models.
	unkToken := "[UNK]"

	// Detect special tokens from the first lines of the vocab file.
	if data, err := os.ReadFile(vocabFile); err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		for i, line := range lines {
			lower := strings.ToLower(line)
			if lower == "<pad>" || lower == "[pad]" {
				padID = int32(i)
			}
			if lower == "<unk>" || lower == "[unk]" {
				unkToken = line
			}
		}
	}

	model, err := wordpiece.NewWordPieceFromFile(vocabFile, unkToken)
	if err != nil {
		return nil, 0, "", fmt.Errorf("load WordPiece model from %s: %w", vocabFile, err)
	}

	tz := sugarme.NewTokenizer(model)

	// Add BertPreTokenizer for WordPiece models (splits text into words).
	bertPretok := pretokenizer.NewBertPreTokenizer()
	tz.WithPreTokenizer(bertPretok)

	return tz, padID, unkToken, nil
}

// loadTokenizerJSON loads a HuggingFace tokenizer from tokenizer.json.
func loadTokenizerJSON(tokenizerPath string, maxLength int) (*sugarme.Tokenizer, error) {
	tz, err := pretrained.FromFile(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer.json from %s: %w", tokenizerPath, err)
	}

	// Validate by encoding a test string.
	testEnc, err := tz.EncodeSingle("test", true)
	if err != nil {
		return nil, fmt.Errorf("validate tokenizer.json: encode test: %w", err)
	}
	if len(testEnc.Ids) == 0 {
		return nil, fmt.Errorf("validate tokenizer.json: empty encoding for test string")
	}

	_ = maxLength // pretrained tokenizer uses its own max length from config

	return tz, nil
}

// Tokenize converts text to a slice of token IDs, padded and truncated to MaxLength.
func (t *SugarmeTokenizer) Tokenize(text string) ([]int32, error) {
	if text == "" {
		return make([]int32, t.maxLength), nil
	}

	inputSeq := sugarme.NewInputSequence(text)
	enc, err := t.tokenizer.Encode(sugarme.NewSingleEncodeInput(inputSeq), false)
	if err != nil {
		return nil, fmt.Errorf("sugarme tokenizer: encode %q: %w", text, err)
	}

	ids := enc.GetIds()

	// Truncate if necessary.
	if len(ids) > t.maxLength {
		ids = ids[:t.maxLength]
	}

	result := make([]int32, 0, len(ids))
	for _, id := range ids {
		result = append(result, int32(id))
	}

	// Pad to maxLength.
	if len(result) < t.maxLength {
		padded := make([]int32, t.maxLength)
		copy(padded, result)
		for i := len(result); i < t.maxLength; i++ {
			padded[i] = t.padID
		}
		result = padded
	}

	return result, nil
}

// Decode converts a slice of token IDs back to text.
// Special tokens are skipped by default.
func (t *SugarmeTokenizer) Decode(tokens []int32) (string, error) {
	if len(tokens) == 0 {
		return "", nil
	}

	ids := make([]int, len(tokens))
	for i, id := range tokens {
		ids[i] = int(id)
	}

	return t.tokenizer.Decode(ids, true), nil
}

// MaxLength returns the configured maximum sequence length.
func (t *SugarmeTokenizer) MaxLength() int {
	return t.maxLength
}
