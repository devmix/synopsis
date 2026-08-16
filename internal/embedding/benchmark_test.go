package embedding

import (
	"math"
	"testing"
)

func TestMedianMS(t *testing.T) {
	tests := []struct {
		name string
		xs   []float64
		want float64
	}{
		{name: "single value", xs: []float64{5}, want: 5},
		{name: "odd count", xs: []float64{3, 1, 2}, want: 2},
		{name: "even count averaged", xs: []float64{4, 1, 3, 2}, want: 2.5},
		{name: "already sorted", xs: []float64{1, 2, 3, 4, 5}, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := medianMS(tt.xs)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("medianMS() = %v, want %v", got, tt.want)
			}
		})
	}

	// Input slice must not be mutated.
	input := []float64{3, 1, 2}
	_ = medianMS(input)
	if input[0] != 3 || input[1] != 1 || input[2] != 2 {
		t.Errorf("medianMS() mutated its input: %v", input)
	}
}

func TestStatsFromMS(t *testing.T) {
	tests := []struct {
		name string
		xs   []float64
		want BenchStats
	}{
		{
			name: "empty sample yields zero stats",
			xs:   nil,
			want: BenchStats{},
		},
		{
			name: "single run is min max and median at once",
			xs:   []float64{12.5},
			want: BenchStats{MedianMS: 12.5, MinMS: 12.5, MaxMS: 12.5, Runs: 1},
		},
		{
			name: "min max and median over unsorted sample",
			xs:   []float64{9, 3, 7, 1, 5},
			want: BenchStats{MedianMS: 5, MinMS: 1, MaxMS: 9, Runs: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statsFromMS(tt.xs)
			if got != tt.want {
				t.Errorf("statsFromMS() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
