package k3s

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// newTestExecInstaller returns an ExecInstaller reading its systemd units from
// a temporary directory instead of /etc/systemd/system.
func newTestExecInstaller(t *testing.T, units ...string) *ExecInstaller {
	t.Helper()

	dir := t.TempDir()
	for _, unit := range units {
		if err := os.WriteFile(filepath.Join(dir, unit), []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatalf("write unit %s: %v", unit, err)
		}
	}

	installer := NewExecInstaller("test-token", "ants-01", slog.New(slog.NewTextHandler(io.Discard, nil)))
	installer.unitDir = dir
	return installer
}

// TestInstalledRole checks that the role of an existing installation is
// recovered from the name of the systemd unit the K3s install script wrote.
func TestInstalledRole(t *testing.T) {
	cases := map[string]struct {
		units []string
		want  node.Role
	}{
		"server": {units: []string{systemdServerUnit}, want: node.RoleServer},
		"agent":  {units: []string{systemdAgentUnit}, want: node.RoleAgent},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := newTestExecInstaller(t, tc.units...).InstalledRole(context.Background())
			if err != nil {
				t.Fatalf("InstalledRole returned error: %v", err)
			}
			if got != tc.want {
				t.Errorf("InstalledRole = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestInstalledRoleWithoutInstallation checks the case of a state file that
// claims a completed first boot on a node where K3s is nowhere to be found.
func TestInstalledRoleWithoutInstallation(t *testing.T) {
	_, err := newTestExecInstaller(t).InstalledRole(context.Background())
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("InstalledRole with no unit: got %v, want ErrNotInstalled", err)
	}
}

// newVaultedInstaller returns an ExecInstaller whose air-gap paths all live under a temporary
// directory, and reports the vault, binary and images paths it was given.
func newVaultedInstaller(t *testing.T) (*ExecInstaller, string, string, string) {
	t.Helper()

	root := t.TempDir()
	installer := newTestExecInstaller(t)
	installer.vaultDir = filepath.Join(root, "vault")
	installer.binPath = filepath.Join(root, "bin", "k3s")
	installer.imagesDir = filepath.Join(root, "images")

	return installer, installer.vaultDir, installer.binPath, installer.imagesDir
}

// writeFile writes content at path, creating the parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestStageAirGapAssets covers what makes a role conversion possible at all: the K3s uninstall
// script deletes the binary and the whole data directory, air-gap image archives included, so a
// node that converted would have nothing left to reinstall from.
func TestStageAirGapAssets(t *testing.T) {
	installer, vault, bin, images := newVaultedInstaller(t)
	writeFile(t, filepath.Join(vault, vaultBinName), "the k3s binary")
	writeFile(t, filepath.Join(vault, vaultImagesName, "k3s-airgap-images-arm64.tar.zst"), "the image archive")

	if err := installer.stageAirGapAssets(context.Background()); err != nil {
		t.Fatalf("stageAirGapAssets: %v", err)
	}

	if got := readFile(t, bin); got != "the k3s binary" {
		t.Errorf("restored binary = %q", got)
	}
	archive := filepath.Join(images, "k3s-airgap-images-arm64.tar.zst")
	if got := readFile(t, archive); got != "the image archive" {
		t.Errorf("restored image archive = %q", got)
	}
}

// TestStageAirGapAssetsWithoutVault pins the refusal to install on a node with no vault: the
// installation would succeed and the node would only discover it is stranded at its first role
// conversion, once its runtime assets have been deleted.
func TestStageAirGapAssetsWithoutVault(t *testing.T) {
	installer, _, bin, _ := newVaultedInstaller(t)
	writeFile(t, bin, "the k3s binary the image shipped")

	if err := installer.stageAirGapAssets(context.Background()); err == nil {
		t.Fatal("stageAirGapAssets without a vault must fail")
	}
}

// TestStageAirGapAssetsKeepsLiveFiles checks that staging never overwrites what is already there:
// it runs before every installation, including the ones on a node whose assets are intact.
func TestStageAirGapAssetsKeepsLiveFiles(t *testing.T) {
	installer, vault, bin, images := newVaultedInstaller(t)
	writeFile(t, filepath.Join(vault, vaultBinName), "the vaulted binary")
	writeFile(t, filepath.Join(vault, vaultImagesName, "images.tar.zst"), "the vaulted archive")
	writeFile(t, bin, "the live binary")
	writeFile(t, filepath.Join(images, "images.tar.zst"), "the live archive")

	if err := installer.stageAirGapAssets(context.Background()); err != nil {
		t.Fatalf("stageAirGapAssets: %v", err)
	}

	if got := readFile(t, bin); got != "the live binary" {
		t.Errorf("binary = %q, want it left untouched", got)
	}
	if got := readFile(t, filepath.Join(images, "images.tar.zst")); got != "the live archive" {
		t.Errorf("image archive = %q, want it left untouched", got)
	}
}

// TestCopyFileLeavesNothingHalfWritten covers the fallback taken when the vault cannot share an
// inode with the runtime path. It must publish the destination in one rename: a half-written file
// there would be read as an asset already in place, by this run and by every later one.
func TestCopyFileLeavesNothingHalfWritten(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "vault", "k3s")
	dst := filepath.Join(dir, "bin", "k3s")
	writeFile(t, src, "the k3s binary")
	// copyFile writes its temp file next to the destination, so that directory must already exist.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(dst), err)
	}

	if err := copyFile(src, dst, 0o755); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	if got := readFile(t, dst); got != "the k3s binary" {
		t.Errorf("copied binary = %q", got)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "bin", "*.tmp*"))
	if err != nil {
		t.Fatalf("glob the destination directory: %v", err)
	}
	if len(leftovers) > 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

// TestConvertRefusesANodeWithoutK3s checks that a conversion asked of a node that has nothing to
// convert stops instead of installing a role beside the state the workflow assumed.
func TestConvertRefusesANodeWithoutK3s(t *testing.T) {
	err := newTestExecInstaller(t).Convert(context.Background(), node.RoleServer, "10.0.0.1")
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Convert on a node with no k3s: got %v, want ErrNotInstalled", err)
	}
}

// TestConvertRefusesAnAmbiguousInstallation is the counterpart: two units mean antsd cannot tell
// which uninstall script to run, so it must not guess.
//
// The error is matched: a Convert that went ahead and ran a script would
// fail too, and the two outcomes must not be confused.
func TestConvertRefusesAnAmbiguousInstallation(t *testing.T) {
	installer := newTestExecInstaller(t, systemdServerUnit, systemdAgentUnit)

	err := installer.Convert(context.Background(), node.RoleAgent, "10.0.0.1")
	if err == nil {
		t.Fatal("Convert accepted an ambiguous installation")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("Convert error = %v, want the refusal raised before any script ran", err)
	}
}

// TestConvertToTheInstalledRoleDoesNothing covers the duplicate order: a conversion the node has
// already performed must not tear a working installation down and put the same one back.
func TestConvertToTheInstalledRoleDoesNothing(t *testing.T) {
	installer := newTestExecInstaller(t, systemdServerUnit)

	if err := installer.Convert(context.Background(), node.RoleServer, "10.0.0.1"); err != nil {
		t.Fatalf("Convert to the role already installed: %v", err)
	}

	// Had the uninstall script been attempted, Convert would have reported the installation it
	// left behind instead of returning cleanly.
	role, err := installer.InstalledRole(context.Background())
	if err != nil || role != node.RoleServer {
		t.Errorf("installed role = %q (error %v), want it left as %q", role, err, node.RoleServer)
	}
}

// TestInstalledRoleAmbiguous checks that leftovers from a previous
// installation are reported rather than silently resolved: antsd has no way to
// tell which of the two units is the live one.
func TestInstalledRoleAmbiguous(t *testing.T) {
	role, err := newTestExecInstaller(t, systemdServerUnit, systemdAgentUnit).InstalledRole(context.Background())
	if err == nil {
		t.Fatalf("InstalledRole accepted an ambiguous installation, returned role %q", role)
	}
	if errors.Is(err, ErrNotInstalled) {
		t.Errorf("ambiguous installation reported as missing: %v", err)
	}
}
