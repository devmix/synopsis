package utils_test

import (
	"testing"

	"github.com/devmix/synopsis/internal/utils"
)

func TestHumanFileSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int64
		want string
	}{
		{
			name: "zero bytes",
			size: 0,
			want: "0 B",
		},
		{
			name: "small file in bytes",
			size: 366,
			want: "366 B",
		},
		{
			name: "kilobytes",
			size: 724923,
			want: "708 KiB",
		},
		{
			name: "megabytes",
			size: 133093490,
			want: "126.9 MiB",
		},
		{
			name: "gigabytes",
			size: 2266820608,
			want: "2.11 GiB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := utils.HumanFileSize(tt.size); got != tt.want {
				t.Errorf("HumanFileSize(%d) = %q, want %q", tt.size, got, tt.want)
			}
		})
	}
}
