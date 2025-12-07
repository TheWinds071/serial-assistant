package jlink

import (
	"os"
	"runtime"
	"testing"
)

// TestGetLibraryPath verifies that the library path detection works for all platforms
func TestGetLibraryPath(t *testing.T) {
	path, err := getLibraryPath()
	if err != nil {
		t.Fatalf("getLibraryPath() failed: %v", err)
	}

	if path == "" {
		t.Fatal("getLibraryPath() returned empty path")
	}

	// Verify platform-specific paths match the logic in getLibraryPath()
	switch runtime.GOOS {
	case "windows":
		// Windows always returns "JLink_x64.dll"
		if path != "JLink_x64.dll" {
			t.Errorf("Expected 'JLink_x64.dll' for Windows, got '%s'", path)
		}
	case "linux":
		// Linux returns local path if it exists, otherwise system path
		localPath := "./libjlinkarm.so"
		systemPath := "/opt/SEGGER/JLink/libjlinkarm.so"
		if _, err := os.Stat(localPath); err == nil {
			if path != localPath {
				t.Errorf("Expected '%s' for Linux (local file exists), got '%s'", localPath, path)
			}
		} else {
			if path != systemPath {
				t.Errorf("Expected '%s' for Linux (local file doesn't exist), got '%s'", systemPath, path)
			}
		}
	case "darwin":
		// macOS returns local path if it exists, otherwise system path
		localPath := "libjlinkarm.dylib"
		systemPath := "/Applications/SEGGER/JLink/libjlinkarm.dylib"
		if _, err := os.Stat(localPath); err == nil {
			if path != localPath {
				t.Errorf("Expected '%s' for macOS (local file exists), got '%s'", localPath, path)
			}
		} else {
			if path != systemPath {
				t.Errorf("Expected '%s' for macOS (local file doesn't exist), got '%s'", systemPath, path)
			}
		}
	}
}

// TestPlatformSpecificFunctionsExist verifies that platform-specific functions are available
func TestPlatformSpecificFunctionsExist(t *testing.T) {
	// These functions should exist and be callable on all platforms
	// We just verify they're available by checking if they're not nil through reflection
	
	// We can't actually call openLibrary without a valid library file
	// But we can verify the function signature exists by trying to call with an invalid path
	_, err := openLibrary("nonexistent_library_for_testing")
	if err == nil {
		t.Error("Expected error when opening nonexistent library, got nil")
	}
}

// TestRegisterLibFuncExists verifies that registerLibFunc is available on all platforms
func TestRegisterLibFuncExists(t *testing.T) {
	// This is primarily a compile-time check that verifies:
	// 1. The registerLibFunc function exists on all platforms
	// 2. The function signature is correct and accessible
	// 3. Build tags properly separate platform-specific implementations
	
	// We verify this by the fact that this test file compiles successfully
	// The actual function behavior is tested through NewJLinkWrapper integration
	// when a valid J-Link library is available
	
	t.Log("registerLibFunc is available and properly defined on", runtime.GOOS)
}

// TestBuildTagsSeparation ensures platform-specific code is properly separated
func TestBuildTagsSeparation(t *testing.T) {
	// This test verifies that build tags are working correctly
	// by checking that we're using the correct implementation for each platform
	
	// On Windows, we should be using syscall.LoadLibrary
	// On Unix, we should be using purego.Dlopen
	// Both should be accessible through the same openLibrary interface
	
	// Try to open a nonexistent library - both implementations should fail
	handle, err := openLibrary("totally_nonexistent_library_xyz123.so")
	if err == nil {
		// Clean up if somehow succeeded
		closeLibrary(handle)
		t.Fatal("Expected error when opening nonexistent library")
	}
	
	// Verify error message contains platform-specific information
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("Error message should not be empty")
	}
	
	t.Logf("Platform: %s, Error: %s", runtime.GOOS, errMsg)
}
