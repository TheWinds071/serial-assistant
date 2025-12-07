package updater

import (
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
