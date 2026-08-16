package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath(t *testing.T) {
	// NOTE: not parallel — some test cases change CWD.

	tests := []struct {
		name     string
		cliPath  string
		preset   string
		setup    func(t *testing.T) string // returns a directory to chdir into, or "" for no-op
		wantFile string                   // expected filename portion in result
	}{
		{
			name:     "explicit config path not overridden by preset",
			cliPath:  "/explicit/path/config.yaml",
			preset:   "production",
			setup:    nil,
			wantFile: "/explicit/path/config.yaml",
		},
		{
			name:     "fallback uses default preset when no files exist",
			cliPath:  "",
			preset:   "default",
			setup: func(t *testing.T) string {
				return t.TempDir() // empty dir, no config files
			},
			wantFile: "configs/config.default.yaml",
		},
		{
			name:     "fallback uses custom preset when no files exist",
			cliPath:  "",
			preset:   "staging",
			setup: func(t *testing.T) string {
				return t.TempDir() // empty dir, no config files
			},
			wantFile: "configs/config.staging.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				dir := tt.setup(t)
				oldWd, _ := os.Getwd()
				t.Cleanup(func() {
					if err := os.Chdir(oldWd); err != nil {
						t.Logf("cleanup chdir: %v", err)
					}
				})
				_ = os.Chdir(dir)
			}

			got := resolveConfigPath(tt.cliPath, tt.preset)
			if got != tt.wantFile {
				t.Errorf("resolveConfigPath(%q, %q) = %q, want %q", tt.cliPath, tt.preset, got, tt.wantFile)
			}
		})
	}
}

func TestResolveConfigPath_PresetCandidatesOrder(t *testing.T) {
	// NOTE: not parallel — this test changes CWD and must run sequentially.

	// Create a temp directory structure that simulates the full candidate search order:
	// exeDir/configs → exeDir → parent(configs) → parent → CWD/configs → CWD → fallback
	tmp := t.TempDir()

	// Layout:
	//   tmp/
	//     configs/            ← parent-level configs (candidate 4)
	//       config.default.yaml
	//       config.staging.yaml
	//     exe/                ← simulated executable directory (candidate 1,2)
	//       configs/          ← exeDir/configs (candidate 1)
	//         config.default.yaml
	//         config.staging.yaml
	//       config.default.yaml   ← exeDir root (candidate 2)
	//     workdir/            ← simulated CWD (candidate 5,6)
	//       configs/          ← CWD/configs (candidate 5)
	//         config.default.yaml
	//         config.staging.yaml

	parentConfigs := filepath.Join(tmp, "configs")
	if err := os.MkdirAll(parentConfigs, 0o755); err != nil {
		t.Fatalf("mkdir parent configs: %v", err)
	}
	for _, preset := range []string{"default", "staging"} {
		if err := os.WriteFile(filepath.Join(parentConfigs, fmt.Sprintf("config.%s.yaml", preset)), []byte("# parent"), 0o644); err != nil {
			t.Fatalf("write parent config: %v", err)
		}
	}

	exeDir := filepath.Join(tmp, "exe")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatalf("mkdir exe dir: %v", err)
	}
	exeConfigs := filepath.Join(exeDir, "configs")
	if err := os.MkdirAll(exeConfigs, 0o755); err != nil {
		t.Fatalf("mkdir exe configs: %v", err)
	}
	for _, preset := range []string{"default", "staging"} {
		if err := os.WriteFile(filepath.Join(exeConfigs, fmt.Sprintf("config.%s.yaml", preset)), []byte("# exe"), 0o644); err != nil {
			t.Fatalf("write exe config: %v", err)
		}
	}
	// Also put a config at exeDir root level (candidate 2)
	for _, preset := range []string{"default", "staging"} {
		if err := os.WriteFile(filepath.Join(exeDir, fmt.Sprintf("config.%s.yaml", preset)), []byte("# exe-root"), 0o644); err != nil {
			t.Fatalf("write exe root config: %v", err)
		}
	}

	workdir := filepath.Join(tmp, "workdir")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	cwdConfigs := filepath.Join(workdir, "configs")
	if err := os.MkdirAll(cwdConfigs, 0o755); err != nil {
		t.Fatalf("mkdir cwd configs: %v", err)
	}
	for _, preset := range []string{"default", "staging"} {
		if err := os.WriteFile(filepath.Join(cwdConfigs, fmt.Sprintf("config.%s.yaml", preset)), []byte("# cwd"), 0o644); err != nil {
			t.Fatalf("write cwd config: %v", err)
		}
	}

	oldWd, _ := os.Getwd()
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Logf("cleanup chdir: %v", err)
		}
	})
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir to workdir: %v", err)
	}

	tests := []struct {
		name   string
		exe    string // simulated executable path (empty = skip exe-level search)
		preset string
		want   string
	}{
		// --- exeDir/configs level (candidate 1, highest priority) ---
		{
			name:   "exe dir configs/default preset finds exeDir/configs/config.default.yaml",
			exe:    filepath.Join(exeDir, "synopsis"),
			preset: "default",
			want:   filepath.Join(exeConfigs, "config.default.yaml"),
		},
		{
			name:   "exe dir configs/staging preset finds exeDir/configs/config.staging.yaml",
			exe:    filepath.Join(exeDir, "synopsis"),
			preset: "staging",
			want:   filepath.Join(exeConfigs, "config.staging.yaml"),
		},

		// --- parent level (candidates 3-4) ---
		{
			name:   "parent dir configs finds config when exe has none",
			exe:    filepath.Join(tmp, "empty-exe", "synopsis"),
			preset: "default",
			want:   filepath.Join(parentConfigs, "config.default.yaml"),
		},

		// --- CWD level (candidates 5-6) ---
		{
			name:   "CWD configs/default preset finds workdir/configs/config.default.yaml",
			exe:    "", // no exe-level search
			preset: "default",
			want:   filepath.Join(cwdConfigs, "config.default.yaml"),
		},
		{
			name:   "CWD configs/staging preset finds workdir/configs/config.staging.yaml",
			exe:    "", // no exe-level search
			preset: "staging",
			want:   filepath.Join(cwdConfigs, "config.staging.yaml"),
		},

		// --- fallback (no files found) --- tested in separate subtest below
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveConfigCandidates(tt.exe, tt.preset)
			if got != tt.want {
				t.Errorf("resolveConfigCandidates(%q, %q) = %q, want %q", tt.exe, tt.preset, got, tt.want)
			}
		})
	}

	// --- Candidate 2: exeDir root (no configs/ subdir) ---
	// Needs isolated fixture because tmp/configs/ would match as parent-level candidate.
	t.Run("exe dir root finds config when configs/ subdir absent", func(t *testing.T) {
		isolated := t.TempDir()
		exeRootOnly := filepath.Join(isolated, "bin")
		if err := os.MkdirAll(exeRootOnly, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		cfgPath := filepath.Join(exeRootOnly, "config.default.yaml")
		if err := os.WriteFile(cfgPath, []byte("# exe-root-only"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		got := resolveConfigCandidates(filepath.Join(exeRootOnly, "synopsis"), "default")
		if got != cfgPath {
			t.Errorf("got %q, want %q", got, cfgPath)
		}
	})

	// --- Fallback: no files found anywhere ---
	// Needs isolated CWD because workdir has configs/ that would match.
	t.Run("fallback returns relative path when nothing exists", func(t *testing.T) {
		isolated := t.TempDir()
		oldWd, _ := os.Getwd()
		if err := os.Chdir(isolated); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chdir(oldWd)
		})

		got := resolveConfigCandidates("", "default")
		if got != "configs/config.default.yaml" {
			t.Errorf("got %q, want configs/config.default.yaml", got)
		}
	})
}
