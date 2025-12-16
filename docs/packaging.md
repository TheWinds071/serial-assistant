# Serial Mate Packaging Guide

This document explains how to build and install Serial Mate packages in different formats (.deb, .rpm, .tar.gz).

## Table of Contents

- [Installation from Packages](#installation-from-packages)
- [Building Packages Locally](#building-packages-locally)
- [Package Contents](#package-contents)
- [Dependencies](#dependencies)

## Installation from Packages

### Fedora 40+ (.rpm with WebKitGTK 4.1)

Download the Fedora `.rpm` package from the [GitHub Releases](https://github.com/TheWinds071/serial-mate/releases) page (look for `.fc40.amd64.rpm`), then install:

```bash
# Using dnf
sudo dnf install serial-mate-<version>-1.fc40.amd64.rpm
```

This package is built on Fedora 41 with WebKitGTK 4.1 and will work on Fedora 40 and newer versions.

To uninstall:

```bash
sudo dnf remove serial-mate
```

### Ubuntu 24.04+ (.deb with WebKitGTK 4.1)

Download the Ubuntu 24.04 `.deb` package from the [GitHub Releases](https://github.com/TheWinds071/serial-mate/releases) page (look for `_ubuntu24.04_amd64.deb`), then install:

```bash
# Install the package
sudo dpkg -i serial-mate_<version>_ubuntu24.04_amd64.deb

# If there are missing dependencies, install them
sudo apt-get install -f
```

This package is built on Ubuntu 24.04 with WebKitGTK 4.1 and requires `libwebkit2gtk-4.1-0`.

To uninstall:

```bash
sudo apt-get remove serial-mate
```

### Ubuntu 22.04 (.deb with WebKitGTK 4.0, legacy)

Download the Ubuntu 22.04 `.deb` package from the [GitHub Releases](https://github.com/TheWinds071/serial-mate/releases) page (look for `_ubuntu22.04_amd64.deb`), then install:

```bash
# Install the package
sudo dpkg -i serial-mate_<version>_ubuntu22.04_amd64.deb

# If there are missing dependencies, install them
sudo apt-get install -f
```

This package is built on Ubuntu 22.04 with WebKitGTK 4.0 and requires `libwebkit2gtk-4.0-37`.

To uninstall:

```bash
sudo apt-get remove serial-mate
```

### Manual Installation (.tar.gz)

Download the `.tar.gz` archive from the [GitHub Releases](https://github.com/TheWinds071/serial-mate/releases) page:

```bash
# Extract the archive
tar -xzf serial-mate-<version>-linux-amd64.tar.gz

# Move binary to a directory in your PATH
sudo mv serial-mate /usr/local/bin/

# Make it executable
sudo chmod +x /usr/local/bin/serial-mate

# Run the application
serial-mate
```

**Note:** Manual installation does not include the desktop entry or icon. You'll need to run the application from the command line or create a desktop entry manually.

To uninstall:

```bash
sudo rm /usr/local/bin/serial-mate
```

## Building Packages Locally

### Prerequisites

1. **Install Go** (version 1.21 or later):
   ```bash
   # Download and install from https://go.dev/dl/
   ```

2. **Install Node.js** (version 20 or later):
   ```bash
   # Download and install from https://nodejs.org/
   ```

3. **Install Wails**:
   ```bash
   go install github.com/wailsapp/wails/v2/cmd/wails@latest
   ```

4. **Install nfpm**:
   ```bash
   go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
   ```

### Building on Fedora 40+ (for RPM with WebKitGTK 4.1)

5. **Install build dependencies on Fedora**:
   ```bash
   sudo dnf install -y golang nodejs npm gcc gcc-c++ make pkgconfig gtk3-devel webkit2gtk4.1-devel
   ```

6. **Build the application**:
   ```bash
   # Clone the repository
   git clone https://github.com/TheWinds071/serial-mate.git
   cd serial-mate
   
   # Build frontend
   cd frontend
   npm install
   npm run build
   cd ..
   
   # Build the Wails application
   wails build -platform linux/amd64 -clean
   
   # Copy binary to dist directory
   mkdir -p dist
   cp build/bin/serial-mate dist/serial-mate
   ```

7. **Validate WebKitGTK linking**:
   ```bash
   # Verify it links to WebKitGTK 4.1
   readelf -d dist/serial-mate | grep -E 'NEEDED.*webkit'
   # Should show libwebkit2gtk-4.1.so
   ```

8. **Generate Fedora RPM**:
   ```bash
   # Set version
   export NFPM_VERSION="1.3.5"
   export NFPM_ARCH="amd64"
   
   # Create output directory
   mkdir -p dist/packages
   
   # Generate .rpm package
   nfpm package \
     --config packaging/nfpm-fedora-rpm.yaml \
     --packager rpm \
     --target dist/packages/serial-mate-${NFPM_VERSION}-1.fc40.amd64.rpm
   ```

### Building on Ubuntu 24.04+ (for DEB with WebKitGTK 4.1)

5. **Install build dependencies on Ubuntu 24.04**:
   ```bash
   sudo apt-get update
   sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
   ```

6. **Build the application**:
   ```bash
   # Clone the repository
   git clone https://github.com/TheWinds071/serial-mate.git
   cd serial-mate
   
   # Build frontend
   cd frontend
   npm install
   npm run build
   cd ..
   
   # Build the Wails application
   wails build -platform linux/amd64 -clean
   
   # Copy binary to dist directory
   mkdir -p dist
   cp build/bin/serial-mate dist/serial-mate
   ```

7. **Validate WebKitGTK linking**:
   ```bash
   # Verify it links to WebKitGTK 4.1
   readelf -d dist/serial-mate | grep -E 'NEEDED.*webkit'
   # Should show libwebkit2gtk-4.1.so
   ```

8. **Generate Ubuntu 24.04 DEB**:
   ```bash
   # Set version
   export NFPM_VERSION="1.3.5"
   export NFPM_ARCH="amd64"
   
   # Create output directory
   mkdir -p dist/packages
   
   # Generate .deb package
   nfpm package \
     --config packaging/nfpm-ubuntu24-deb.yaml \
     --packager deb \
     --target dist/packages/serial-mate_${NFPM_VERSION}_ubuntu24.04_amd64.deb
   ```

### Building on Ubuntu 22.04 (for DEB with WebKitGTK 4.0, legacy)

5. **Install build dependencies on Ubuntu 22.04**:
   ```bash
   sudo apt-get update
   sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.0-dev
   ```

6. **Build the application**:
   ```bash
   # Clone the repository
   git clone https://github.com/TheWinds071/serial-mate.git
   cd serial-mate
   
   # Build frontend
   cd frontend
   npm install
   npm run build
   cd ..
   
   # Build the Wails application
   wails build -platform linux/amd64 -clean
   
   # Copy binary to dist directory
   mkdir -p dist
   cp build/bin/serial-mate dist/serial-mate
   ```

7. **Generate Ubuntu 22.04 DEB**:
   ```bash
   # Set version
   export NFPM_VERSION="1.3.5"
   export NFPM_ARCH="amd64"
   
   # Create output directory
   mkdir -p dist/packages
   
   # Generate .deb package
   nfpm package \
     --config packaging/nfpm.yaml \
     --packager deb \
     --target dist/packages/serial-mate_${NFPM_VERSION}_ubuntu22.04_amd64.deb
   ```

### Generate tar.gz Archive

```bash
# Generate .tar.gz archive (can be done from any build)
cd dist
tar -czf packages/serial-mate-${NFPM_VERSION}-linux-amd64.tar.gz serial-mate
cd ..
```

### Build Output

Packages will be available in `dist/packages/`:
- `serial-mate-<version>-1.fc40.amd64.rpm` (Fedora 40+ with WebKitGTK 4.1)
- `serial-mate_<version>_ubuntu24.04_amd64.deb` (Ubuntu 24.04+ with WebKitGTK 4.1)
- `serial-mate_<version>_ubuntu22.04_amd64.deb` (Ubuntu 22.04 with WebKitGTK 4.0)
- `serial-mate-<version>-linux-amd64.tar.gz` (binary only)

## Package Contents

All packages (except .tar.gz) install the following files:

| File | Path | Description |
|------|------|-------------|
| Binary | `/usr/bin/serial-mate` | Main application executable |
| Desktop Entry | `/usr/share/applications/serial-mate.desktop` | Desktop application launcher |
| Icon | `/usr/share/pixmaps/serial-mate.png` | Application icon |

The `.tar.gz` archive contains only the binary file.

## Dependencies

### Runtime Dependencies

Serial Mate requires the following runtime dependencies depending on the distribution and WebKitGTK version:

**Fedora 40+ (WebKitGTK 4.1):**
- `gtk3` - GTK+ 3 library
- `webkit2gtk4.1` - WebKit2GTK 4.1 library

**Ubuntu 24.04+ (WebKitGTK 4.1):**
- `libgtk-3-0` - GTK+ 3 library
- `libwebkit2gtk-4.1-0` - WebKit2GTK 4.1 library

**Ubuntu 22.04 (WebKitGTK 4.0, legacy):**
- `libgtk-3-0` - GTK+ 3 library
- `libwebkit2gtk-4.0-37` - WebKit2GTK 4.0 library

These dependencies are automatically installed when using `.deb` or `.rpm` packages.

### Build Dependencies

For building from source, you need:

**Fedora 40+ (WebKitGTK 4.1):**
- `gtk3-devel`
- `webkit2gtk4.1-devel`
- `gcc`, `gcc-c++`, `make`, `pkgconfig`
- `golang`, `nodejs`, `npm`

**Ubuntu 24.04+ (WebKitGTK 4.1):**
- `libgtk-3-dev`
- `libwebkit2gtk-4.1-dev`
- `build-essential`
- `golang`, `nodejs`, `npm`

**Ubuntu 22.04 (WebKitGTK 4.0, legacy):**
- `libgtk-3-dev`
- `libwebkit2gtk-4.0-dev`
- `build-essential`
- `golang`, `nodejs`, `npm`

## WebKitGTK Version Compatibility

Serial Mate is built against different WebKitGTK versions depending on the target distribution:

- **Fedora 40+**: Uses WebKitGTK 4.1 (webkit2gtk4.1)
- **Ubuntu 24.04+**: Uses WebKitGTK 4.1 (libwebkit2gtk-4.1-0)
- **Ubuntu 22.04**: Uses WebKitGTK 4.0 (libwebkit2gtk-4.0-37)

**Important:** Make sure to download the correct package for your distribution. Using the wrong package will result in missing library errors at runtime.

## Troubleshooting

### Permission Denied

If you get "Permission denied" when running serial-mate, ensure the binary is executable:

```bash
sudo chmod +x /usr/bin/serial-mate
```

### Missing Dependencies / Library Not Found

If the application fails to start with errors like "libwebkit2gtk-4.1.so.0: cannot open shared object file" or similar:

1. **Verify you're using the correct package for your distribution:**
   - Fedora 40+: Use `.fc40.amd64.rpm`
   - Ubuntu 24.04+: Use `_ubuntu24.04_amd64.deb`
   - Ubuntu 22.04: Use `_ubuntu22.04_amd64.deb`

2. **Install missing dependencies:**

   **Fedora:**
   ```bash
   sudo dnf install gtk3 webkit2gtk4.1
   ```

   **Ubuntu 24.04+:**
   ```bash
   sudo apt-get install -f
   # Or manually:
   sudo apt-get install libgtk-3-0 libwebkit2gtk-4.1-0
   ```

   **Ubuntu 22.04:**
   ```bash
   sudo apt-get install -f
   # Or manually:
   sudo apt-get install libgtk-3-0 libwebkit2gtk-4.0-37
   ```

### Desktop Entry Not Showing

If the application doesn't appear in your application menu:

```bash
# Update desktop database
sudo update-desktop-database
```

## Contributing

If you encounter any issues with packaging or have suggestions for improvements, please open an issue on the [GitHub repository](https://github.com/TheWinds071/serial-mate/issues).

## License

Serial Mate is licensed under the GNU General Public License v3.0 (GPL-3.0). See the [LICENSE](../LICENSE) file for details.
