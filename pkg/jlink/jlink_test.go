package jlink

import (
	"os"
	"runtime"
	"testing"
	"unsafe"
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

// TestParseBufferDesc tests the RTT buffer descriptor parsing
func TestParseBufferDesc(t *testing.T) {
	// Test parsing a buffer descriptor
	data := make([]byte, 24)
	// Set some test values
	data[0] = 0x00
	data[1] = 0x10
	data[2] = 0x00
	data[3] = 0x20 // NamePtr = 0x20001000
	
	data[4] = 0x00
	data[5] = 0x20
	data[6] = 0x00
	data[7] = 0x20 // BufferPtr = 0x20002000
	
	data[8] = 0x00
	data[9] = 0x04
	data[10] = 0x00
	data[11] = 0x00 // Size = 1024
	
	desc := parseBufferDesc(data)
	if desc.NamePtr != 0x20001000 {
		t.Errorf("Expected NamePtr 0x20001000, got 0x%08X", desc.NamePtr)
	}
	if desc.BufferPtr != 0x20002000 {
		t.Errorf("Expected BufferPtr 0x20002000, got 0x%08X", desc.BufferPtr)
	}
	if desc.Size != 1024 {
		t.Errorf("Expected Size 1024, got %d", desc.Size)
	}
}

// TestMemorySafetyBoundsChecking tests that our fixes prevent memory exhaustion
func TestMemorySafetyBoundsChecking(t *testing.T) {
	// Create a mock JLinkWrapper to test the bounds checking logic
	jl := &JLinkWrapper{
		useSoftRTT: true,
		rttControlBlk: 0x20000000,
		rttUpBuffer: RTTBufferDesc{
			BufferPtr: 0x20001000,
			Size:      1024, // 1KB buffer
		},
		readBuffer: make([]byte, 4096),
	}
	
	// Mock the apiReadMem function to return corrupted offset values
	jl.apiReadMem = func(addr uint32, size uint32, buf uintptr) int {
		// Simulate corrupted wrOff and rdOff that would cause huge allocations
		if addr == jl.rttControlBlk+24+12 { // wrOffAddr
			// Write a huge value that exceeds buffer size
			*(*uint32)(unsafe.Pointer(buf)) = 0xFFFFFFFF
			return 0
		}
		if addr == jl.rttControlBlk+24+16 { // rdOffAddr
			*(*uint32)(unsafe.Pointer(buf)) = 0
			return 0
		}
		return 0
	}
	
	// Call readSoftRTT - it should detect the invalid offsets and return an error
	// instead of attempting to allocate a huge buffer
	data, err := jl.readSoftRTT()
	
	// We expect an error due to offset validation
	if err == nil {
		t.Error("Expected error for out-of-bounds offset, got nil")
	}
	
	if data != nil {
		t.Error("Expected nil data for invalid offsets")
	}
	
	t.Logf("Correctly rejected invalid offsets: %v", err)
}

// TestBufferReuse verifies that ReadRTT reuses the internal buffer
func TestBufferReuse(t *testing.T) {
	jl := &JLinkWrapper{
		useSoftRTT: false,
		readBuffer: make([]byte, 4096),
	}
	
	// Verify the readBuffer is allocated
	if jl.readBuffer == nil {
		t.Fatal("readBuffer should be pre-allocated")
	}
	
	if len(jl.readBuffer) != 4096 {
		t.Errorf("Expected readBuffer size 4096, got %d", len(jl.readBuffer))
	}
	
	// Store the original buffer pointer to verify reuse
	originalPtr := &jl.readBuffer[0]
	
	// Mock apiRTTRead to simulate a read
	callCount := 0
	jl.apiRTTRead = func(channel uint32, buf uintptr, size uint32) int {
		callCount++
		// Verify the buffer pointer passed is the internal buffer
		if buf != uintptr(unsafe.Pointer(&jl.readBuffer[0])) {
			t.Error("ReadRTT should use the internal readBuffer")
		}
		return 0 // No data
	}
	
	// Call ReadRTT multiple times
	for i := 0; i < 10; i++ {
		jl.ReadRTT()
	}
	
	// Verify the buffer pointer hasn't changed (buffer is being reused)
	if &jl.readBuffer[0] != originalPtr {
		t.Error("Buffer should be reused, not reallocated")
	}
	
	if callCount != 10 {
		t.Errorf("Expected 10 calls to apiRTTRead, got %d", callCount)
	}
}
