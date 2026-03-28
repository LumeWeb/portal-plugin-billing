package gateway

import (
	"fmt"
	"io/fs"
)

// ReadGatewayLogo reads a gateway logo file from an embedded filesystem.
//
// Parameters:
//   - gatewayID: The ID of the gateway (e.g., "stripe", "atlos")
//   - logoFS: The embedded filesystem containing logo assets
//   - testOverride: Optional filesystem override for testing (nil for normal operation)
//
// Returns:
//   - []byte: The logo file content
//   - error: An error if the file cannot be read
//
// The function will look for the logo at "assets/{gatewayID}.svg"
func ReadGatewayLogo(gatewayID string, logoFS fs.FS, testOverride fs.FS) ([]byte, error) {
	// Use test override if provided, otherwise use the embedded FS
	var defaultFS fs.FS = logoFS
	if testOverride != nil {
		defaultFS = testOverride
	}

	// Type assert to ReadFileFS to use ReadFile method
	readFileFS, ok := defaultFS.(fs.ReadFileFS)
	if !ok {
		return nil, fmt.Errorf("filesystem does not support reading files")
	}

	// Read the logo file
	filePath := "assets/" + gatewayID + ".svg"
	file, err := readFileFS.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read logo file %s: %w", filePath, err)
	}

	return file, nil
}
