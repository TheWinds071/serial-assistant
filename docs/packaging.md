# Serial Mate Packaging Guide

This document explains how to build and install Serial Mate packages in different formats (.deb, .rpm, .tar.gz).

## Table of Contents

- [Installation from Packages](#installation-from-packages)
- [Building Packages Locally](#building-packages-locally)
- [Package Contents](#package-contents)
- [Dependencies](#dependencies)

## Installation from Packages

### Debian/Ubuntu (.deb)

Download the `.deb` package from the [GitHub Releases](https://github.com/TheWinds071/serial-mate/releases) page, then install:

```bash
# Install the package
sudo dpkg -i serial-mate_<version>_amd64.deb

# If there are missing dependencies, install them
sudo apt-get install -f
```

To uninstall:

```bash
sudo apt-get remove serial-mate
```

### RHEL/Fedora/CentOS (.rpm)

Download the `.rpm` package from the [GitHub Releases](https://github.com/TheWinds071/serial-mate/releases) page, then install:

```bash
# Using rpm
sudo rpm -i serial-mate-<version>-1.amd64.rpm

# Or using dnf (Fedora/newer RHEL)
sudo dnf install serial-mate-<version>-1.amd64.rpm

# Or using yum (older RHEL/CentOS)
sudo yum install serial-mate-<version>-1.amd64.rpm
```

To uninstall:

```bash
# Using rpm
sudo rpm -e serial-mate

# Or using dnf/yum
sudo dnf remove serial-mate
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
   curl -sfL https://goreleaser.com/static/run | bash -s -- install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
   ```

5. **Install Linux build dependencies** (Ubuntu/Debian):
   ```bash
   sudo apt-get update
   sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.0-dev
   ```

### Build Process

1. **Clone the repository**:
   ```bash
   git clone https://github.com/TheWinds071/serial-mate.git
   cd serial-mate
   ```

2. **Build the application**:
   ```bash
   wails build -platform linux/amd64 -clean
   ```

3. **Generate packages**:
   ```bash
   # Set version (or use git tag)
   export NFPM_VERSION="1.3.5"
   export NFPM_ARCH="amd64"
   export NFPM_BINARY_PATH="./build/bin/serial-mate"
   
   # Create output directory
   mkdir -p dist/packages
   
   # Generate .deb package
   nfpm package \
     --config packaging/nfpm.yaml \
     --packager deb \
     --target dist/packages/serial-mate_${NFPM_VERSION}_${NFPM_ARCH}.deb
   
   # Generate .rpm package
   nfpm package \
     --config packaging/nfpm.yaml \
     --packager rpm \
     --target dist/packages/serial-mate-${NFPM_VERSION}-1.${NFPM_ARCH}.rpm
   
   # Generate .tar.gz archive
   cd build/bin
   tar -czf ../../dist/packages/serial-mate-${NFPM_VERSION}-linux-${NFPM_ARCH}.tar.gz serial-mate
   cd ../..
   ```

4. **Packages will be available in** `dist/packages/`:
   - `serial-mate_<version>_amd64.deb`
   - `serial-mate-<version>-1.amd64.rpm`
   - `serial-mate-<version>-linux-amd64.tar.gz`

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

Serial Mate requires the following runtime dependencies:

**Debian/Ubuntu:**
- `libgtk-3-0` - GTK+ 3 library
- `libwebkit2gtk-4.0-37` - WebKit2GTK library

**RHEL/Fedora/CentOS:**
- `gtk3` - GTK+ 3 library
- `webkit2gtk4.1` - WebKit2GTK library

These dependencies are automatically installed when using `.deb` or `.rpm` packages.

### Build Dependencies

For building from source, you need:

**Debian/Ubuntu:**
- `libgtk-3-dev`
- `libwebkit2gtk-4.0-dev`

**RHEL/Fedora/CentOS:**
- `gtk3-devel`
- `webkit2gtk4.1-devel`

## Troubleshooting

### Permission Denied

If you get "Permission denied" when running serial-mate, ensure the binary is executable:

```bash
sudo chmod +x /usr/bin/serial-mate
```

### Missing Dependencies

If the application fails to start due to missing dependencies:

**Debian/Ubuntu:**
```bash
sudo apt-get install -f
```

**RHEL/Fedora/CentOS:**
```bash
sudo dnf install gtk3 webkit2gtk4.1
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
