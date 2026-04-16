package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotenv_SetsUnsetKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# a comment
KEY_A=value-a
KEY_B="value b with spaces"
KEY_C='single-quoted'

# blank line above
KEY_D=no-quotes
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, k := range []string{"KEY_A", "KEY_B", "KEY_C", "KEY_D"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}

	loadDotenv(path)

	cases := map[string]string{
		"KEY_A": "value-a",
		"KEY_B": "value b with spaces",
		"KEY_C": "single-quoted",
		"KEY_D": "no-quotes",
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadDotenv_DoesNotOverrideExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ALREADY_SET=from-file\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("ALREADY_SET", "from-env")

	loadDotenv(path)

	if got := os.Getenv("ALREADY_SET"); got != "from-env" {
		t.Errorf("ALREADY_SET = %q, want %q (loadDotenv must not overwrite)", got, "from-env")
	}
}

func TestLoadDotenv_MissingFileIsSilent(t *testing.T) {
	loadDotenv(filepath.Join(t.TempDir(), "does-not-exist"))
}

func TestLoadDotenv_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `no-equals-sign
=no-key
VALID=yes
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = os.Unsetenv("VALID")

	loadDotenv(path)

	if got := os.Getenv("VALID"); got != "yes" {
		t.Errorf("VALID = %q, want %q", got, "yes")
	}
}
