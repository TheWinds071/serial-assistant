package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	GitHubRepo   = "TheWinds071/serial-mate"
	CheckTimeout = 10 * time.Second
	// OldExeCleanupDelay is the delay before cleaning up the old executable after update
	OldExeCleanupDelay = 5 * time.Second
)

// Release represents a GitHub release
type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
	PublishedAt time.Time `json:"published_at"`
}

// UpdateInfo contains information about an available update
type UpdateInfo struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	ReleaseNotes   string `json:"releaseNotes"`
	DownloadURL    string `json:"downloadUrl"`
	AssetSize      int64  `json:"assetSize"`
}

// CheckForUpdates checks if a new version is available on GitHub
func CheckForUpdates(currentVersion string) (*UpdateInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GitHubRepo)

	client := &http.Client{Timeout: CheckTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent to avoid rate limiting
	req.Header.Set("User-Agent", "serial-mate-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode release: %w", err)
	}

	info := &UpdateInfo{
		CurrentVersion: currentVersion,
		LatestVersion:  release.TagName,
		ReleaseNotes:   release.Body,
	}

	// Compare versions (simple string comparison, assuming semver format v1.2.3)
	if compareVersions(release.TagName, currentVersion) > 0 {
		info.Available = true

		// Find the appropriate asset for the current platform
		assetName := getAssetName()
		for _, asset := range release.Assets {
			if asset.Name == assetName {
				info.DownloadURL = asset.BrowserDownloadURL
				info.AssetSize = asset.Size
				break
			}
		}

		if info.DownloadURL == "" {
			return nil, fmt.Errorf("no compatible asset found for platform")
		}
	}

	return info, nil
}

// DownloadUpdate downloads the update file
func DownloadUpdate(downloadURL string, progressCallback func(downloaded, total int64)) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Create temporary file
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, filepath.Base(downloadURL))

	out, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	// Download with progress
	totalSize := resp.ContentLength
	downloaded := int64(0)
	buffer := make([]byte, 32*1024)

	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := out.Write(buffer[:n]); writeErr != nil {
				return "", fmt.Errorf("failed to write to file: %w", writeErr)
			}
			downloaded += int64(n)
			if progressCallback != nil {
				progressCallback(downloaded, totalSize)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}
	}

	return tmpFile, nil
}

// InstallUpdate installs the downloaded update
func InstallUpdate(updateFile string) error {
	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	// For both Windows and Unix, we use copy + remove to handle cross-device moves
	// (rename fails with "invalid cross-device link" when source and dest are on different filesystems)
	if runtime.GOOS == "windows" {
		// Rename old executable
		oldPath := exePath + ".old"
		if err := os.Rename(exePath, oldPath); err != nil {
			return fmt.Errorf("failed to backup old executable: %w", err)
		}

		// Copy new executable
		if err := copyFile(updateFile, exePath); err != nil {
			// Restore old executable on failure
			if restoreErr := os.Rename(oldPath, exePath); restoreErr != nil {
				return fmt.Errorf("failed to install update and restore failed: %w (restore error: %v)", err, restoreErr)
			}
			return fmt.Errorf("failed to install update: %w", err)
		}

		// Clean up old executable in background
		// Note: We ignore cleanup errors as they don't affect functionality
		// The old file is just a backup and can be removed manually if needed
		go func() {
			time.Sleep(OldExeCleanupDelay)
			_ = os.Remove(oldPath) // Ignore error - cleanup is best-effort
		}()
	} else {
		// For Unix systems, use copy + remove instead of rename to handle cross-device moves
		// Rename old executable as backup
		oldPath := exePath + ".old"
		if err := os.Rename(exePath, oldPath); err != nil {
			return fmt.Errorf("failed to backup old executable: %w", err)
		}

		// Copy new executable
		if err := copyFile(updateFile, exePath); err != nil {
			// Restore old executable on failure
			if restoreErr := os.Rename(oldPath, exePath); restoreErr != nil {
				return fmt.Errorf("failed to install update and restore failed: %w (restore error: %v)", err, restoreErr)
			}
			return fmt.Errorf("failed to install update: %w", err)
		}

		// Make the new executable have executable permissions
		if err := os.Chmod(exePath, 0755); err != nil {
			// Restore old executable on failure
			if restoreErr := os.Rename(oldPath, exePath); restoreErr != nil {
				return fmt.Errorf("failed to set executable permissions and restore failed: %w (restore error: %v)", err, restoreErr)
			}
			return fmt.Errorf("failed to set executable permissions: %w", err)
		}

		// Remove the temporary update file
		_ = os.Remove(updateFile) // Best effort cleanup

		// Clean up old executable in background
		go func() {
			time.Sleep(OldExeCleanupDelay)
			_ = os.Remove(oldPath) // Ignore error - cleanup is best-effort
		}()
	}

	return nil
}

// getAssetName returns the asset name for the current platform
func getAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "serial-mate-windows-amd64.exe"
	case "darwin":
		return "serial-mate-macos-universal.app.zip"
	case "linux":
		return "serial-mate-linux-amd64"
	default:
		return ""
	}
}

// compareVersions compares two version strings (v1.2.3 format)
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
// Note: Invalid version parts are treated as 0 for comparison purposes
func compareVersions(v1, v2 string) int {
	// Remove 'v' prefix if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	// Pad with zeros if needed
	for len(parts1) < maxLen {
		parts1 = append(parts1, "0")
	}
	for len(parts2) < maxLen {
		parts2 = append(parts2, "0")
	}

	for i := 0; i < maxLen; i++ {
		n1, err1 := strconv.Atoi(parts1[i])
		if err1 != nil {
			n1 = 0 // Treat invalid parts as 0
		}
		n2, err2 := strconv.Atoi(parts2[i])
		if err2 != nil {
			n2 = 0 // Treat invalid parts as 0
		}

		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	return 0
}

// copyFile copies a file from src to dst with secure permissions
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Create destination file with secure permissions (owner read/write, group/other read)
	destFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Sync to ensure data is written to disk
	return destFile.Sync()
}

// RestartApplication restarts the application after a delay
// This function spawns a new process and exits the current one
func RestartApplication(delaySeconds int) error {
	// Validate delay parameter to prevent excessively long delays or negative values
	if delaySeconds < 1 || delaySeconds > 60 {
		return fmt.Errorf("delay must be between 1 and 60 seconds, got %d", delaySeconds)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	if runtime.GOOS == "windows" {
		// For Windows, use cmd.exe to delay and restart
		// Use /C to execute the command and close, ping for delay, and start to launch the app
		// Note: ping -n counts include the first immediate ping, so we add 1 to match the delay
		// The executable path is properly quoted to handle spaces and special characters
		cmd := exec.Command("cmd", "/C", fmt.Sprintf("ping 127.0.0.1 -n %d > nul && start \"\" \"%s\"", delaySeconds+1, escapeWindowsPath(exePath)))
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to schedule restart: %w", err)
		}
	} else {
		// For Unix-like systems, use a shell script with sleep and exec
		// Use proper shell escaping to prevent command injection
		cmd := exec.Command("sh", "-c", fmt.Sprintf("sleep %d && exec %s &", delaySeconds, escapeShellArg(exePath)))
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to schedule restart: %w", err)
		}
	}

	return nil
}

// escapeWindowsPath escapes a Windows path for use in cmd.exe commands
// Replaces special characters that could break the command
func escapeWindowsPath(path string) string {
	// For Windows paths in double quotes, we need to escape:
	// - Double quotes (") -> ""
	// - Percent signs (%) -> %%
	path = strings.ReplaceAll(path, "\"", "\"\"")
	path = strings.ReplaceAll(path, "%", "%%")
	return path
}

// escapeShellArg escapes a string for safe use as a shell argument
// Uses single quotes to prevent variable expansion and command substitution
func escapeShellArg(arg string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote)
	// Then wrap the whole string in single quotes
	escaped := strings.ReplaceAll(arg, "'", "'\\''")
	return "'" + escaped + "'"
}
