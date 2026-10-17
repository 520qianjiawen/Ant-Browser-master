//go:build !windows
// +build !windows

package browser



func installCRXIntoProfile(userDataDir string, _ string, packagePath string, extension Extension, _ []string) (string, error) {
	return installExtensionPackageIntoProfile(userDataDir, packagePath, extension)
}

func browserExtensionInstallerExitHint(_ string) string {
	return ""
}

func (m *Manager) cleanupManagedExternalExtensionRegistry() error {
	return nil
}

func ensurePersistentExternalExtensionRegistry(runtimeID string, packagePath string, version string) error {
	return nil
}
