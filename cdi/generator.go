package cdi

import (
	"fmt"
	"os"
	"path/filepath"

	"tags.cncf.io/container-device-interface/pkg/cdi"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"
)

const (
	DefaultSpecDir     = cdi.DefaultStaticDir
	DefaultPermissions = 0o644
)

// Generator writes a CDI spec to a file via the CDI cache.
type Generator struct {
	version  string
	specDir  string
	specFile string
}

// NewGenerator creates a CDI spec generator targeting specDir/specFile.
func NewGenerator(specDir, specFile string) *Generator {
	return &Generator{version: DefaultVersion, specDir: specDir, specFile: specFile}
}

// Write writes the CDI spec to specDir/specFile (0644).
func (g *Generator) Write(spec *cdiSpecs.Spec) error {
	if err := os.MkdirAll(g.specDir, 0o755); err != nil {
		return fmt.Errorf("create spec directory: %w", err)
	}
	cache, err := cdi.NewCache(cdi.WithAutoRefresh(false), cdi.WithSpecDirs(g.specDir))
	if err != nil {
		return fmt.Errorf("create CDI cache: %w", err)
	}
	if err := cache.WriteSpec(spec, g.specFile); err != nil {
		return fmt.Errorf("write spec file: %w", err)
	}
	specPath := filepath.Join(g.specDir, g.specFile)
	if err := os.Chmod(specPath, DefaultPermissions); err != nil {
		return fmt.Errorf("set file permissions: %w", err)
	}
	return nil
}

// Remove deletes the CDI spec file if it exists.
func (g *Generator) Remove() error {
	specPath := filepath.Join(g.specDir, g.specFile)
	if err := os.Remove(specPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove spec file: %w", err)
	}
	return nil
}
