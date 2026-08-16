package relations

import "context"

// MockCrossDomainLinker implements CrossDomainLinker for testing.
type MockCrossDomainLinker struct {
	Decide        func(a, b EntityCandidate) (*LinkDecision, error)
	CalledInOrder []struct{ A, B EntityCandidate }
}

// Name returns the linker identifier.
func (m *MockCrossDomainLinker) Name() string { return "mock" }

// LinkPair records the call and delegates to Decide.
func (m *MockCrossDomainLinker) LinkPair(_ context.Context, a, b EntityCandidate) (*LinkDecision, error) {
	m.CalledInOrder = append(m.CalledInOrder, struct{ A, B EntityCandidate }{A: a, B: b})
	if m.Decide == nil {
		return &LinkDecision{SameEntity: false, Confidence: 0.5}, nil
	}
	return m.Decide(a, b)
}
