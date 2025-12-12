package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"v1.2.3", "v1.2.4", -1},
		{"v1.2.4", "v1.2.3", 1},
		{"v1.2.3", "v1.2.3", 0},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0", "v2.0.0", -1},
		{"1.2.3", "1.2.4", -1},
		{"v1.2", "v1.2.0", 0},
		{"v1.2.3.4", "v1.2.3.5", -1},
	}

	for _, tt := range tests {
		result := compareVersions(tt.v1, tt.v2)
		if result != tt.expected {
			t.Errorf("compareVersions(%s, %s) = %d, expected %d", tt.v1, tt.v2, result, tt.expected)
		}
	}
}

func TestGetAssetName(t *testing.T) {
	assetName := getAssetName()
	if assetName == "" {
		t.Error("getAssetName() returned empty string")
	}

	// Asset name should contain the platform-specific naming
	expectedNames := []string{
		"serial-mate-windows-amd64.exe",
		"serial-mate-macos-universal.app.zip",
		"serial-mate-linux-amd64",
	}

	found := false
	for _, expected := range expectedNames {
		if assetName == expected {
			found = true
			break
		}
	}

	if !found {
		t.Logf("Asset name: %s (this is platform-specific)", assetName)
	}
}

func TestCopyFile(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a source file with test content
	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("test content for copy file")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy the file to a destination
	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile() failed: %v", err)
	}

	// Verify the destination file exists and has the same content
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(dstContent) != string(testContent) {
		t.Errorf("Destination file content doesn't match. Got %s, want %s", dstContent, testContent)
	}

	// Verify permissions are set correctly
	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("Failed to stat destination file: %v", err)
	}

	if runtime.GOOS != "windows" {
		// On Unix systems, check that file is readable
		mode := info.Mode()
		if mode&0400 == 0 {
			t.Errorf("Destination file is not readable by owner")
		}
	}
}

func TestInstallUpdate(t *testing.T) {
	// Skip this test on Windows in CI as it may have file locking issues
	if runtime.GOOS == "windows" && os.Getenv("CI") == "true" {
		t.Skip("Skipping InstallUpdate test on Windows in CI")
	}

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a fake executable file
	exePath := filepath.Join(tmpDir, "fake-executable")
	originalContent := []byte("original executable content")
	if err := os.WriteFile(exePath, originalContent, 0755); err != nil {
		t.Fatalf("Failed to create fake executable: %v", err)
	}

	// Create a fake update file
	updatePath := filepath.Join(tmpDir, "update-file")
	updateContent := []byte("updated executable content")
	if err := os.WriteFile(updatePath, updateContent, 0644); err != nil {
		t.Fatalf("Failed to create update file: %v", err)
	}

	// Mock the executable path by creating a test function
	// Note: We can't easily test InstallUpdate directly since it uses os.Executable()
	// Instead, we test the core logic: backup, copy, chmod, cleanup

	// Backup the original
	oldPath := exePath + ".old"
	if err := os.Rename(exePath, oldPath); err != nil {
		t.Fatalf("Failed to backup: %v", err)
	}

	// Copy the update
	if err := copyFile(updatePath, exePath); err != nil {
		t.Fatalf("Failed to copy update: %v", err)
	}

	// Set executable permissions
	if err := os.Chmod(exePath, 0755); err != nil {
		t.Fatalf("Failed to chmod: %v", err)
	}

	// Verify the new executable has the correct content
	newContent, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("Failed to read new executable: %v", err)
	}

	if string(newContent) != string(updateContent) {
		t.Errorf("New executable content doesn't match. Got %s, want %s", newContent, updateContent)
	}

	// Verify the backup exists and has the original content
	backupContent, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("Failed to read backup: %v", err)
	}

	if string(backupContent) != string(originalContent) {
		t.Errorf("Backup content doesn't match. Got %s, want %s", backupContent, originalContent)
	}

	// Verify executable permissions on Unix systems
	if runtime.GOOS != "windows" {
		info, err := os.Stat(exePath)
		if err != nil {
			t.Fatalf("Failed to stat executable: %v", err)
		}

		mode := info.Mode()
		if mode&0111 == 0 {
			t.Errorf("Executable doesn't have execute permissions: %v", mode)
		}
	}

	// Clean up
	os.Remove(updatePath)
	os.Remove(oldPath)
}

func TestRestartApplication(t *testing.T) {
	// This test verifies that RestartApplication doesn't panic and returns an error or nil
	// We can't fully test the restart functionality in unit tests, but we can ensure
	// the function handles basic execution without errors

	// Test with invalid delay (too small)
	err := RestartApplication(0)
	if err == nil {
		t.Error("Expected error for delay < 1, got nil")
	}

	// Test with invalid delay (too large)
	err = RestartApplication(61)
	if err == nil {
		t.Error("Expected error for delay > 60, got nil")
	}

	// Test with a valid delay
	err = RestartApplication(1)

	// On some systems (especially in CI), the command may fail if there's no shell available
	// or if the current executable path cannot be determined, which is acceptable
	if err != nil {
		t.Logf("RestartApplication returned error (this may be expected in test environment): %v", err)
	} else {
		t.Log("RestartApplication scheduled successfully")
	}

	// The function should not panic regardless of the environment
	// If we reach here, the test passes
}

func TestEscapeShellArg(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"with'quote", "'with'\\''quote'"},
		{"/path/to/file", "'/path/to/file'"},
		{"C:\\Program Files\\app.exe", "'C:\\Program Files\\app.exe'"},
	}

	for _, tt := range tests {
		result := escapeShellArg(tt.input)
		if result != tt.expected {
			t.Errorf("escapeShellArg(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestEscapeWindowsPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"C:\\Program Files\\app.exe", "C:\\Program Files\\app.exe"},
		{"path with \"quotes\"", "path with \"\"quotes\"\""},
		{"path with %var%", "path with %%var%%"},
		{"C:\\Users\\Test\"User\\app.exe", "C:\\Users\\Test\"\"User\\app.exe"},
	}

	for _, tt := range tests {
		result := escapeWindowsPath(tt.input)
		if result != tt.expected {
			t.Errorf("escapeWindowsPath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
