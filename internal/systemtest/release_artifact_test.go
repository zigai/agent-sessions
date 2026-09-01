//go:build integration

package systemtest

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	artifactDirEnv              = "AHT_ARTIFACT_DIR"
	publishedArtifactDirEnv     = "AHT_PUBLISHED_ARTIFACT_DIR"
	artifactNegativeControlsEnv = "AHT_ARTIFACT_NEGATIVE_CONTROLS"
)

type releaseArtifactExtra struct {
	Binary    string          `json:"Binary"`
	Binaries  []string        `json:"Binaries"`
	Builder   string          `json:"Builder"`
	Checksum  string          `json:"Checksum"`
	Ext       string          `json:"Ext"`
	Files     json.RawMessage `json:"Files"`
	Format    string          `json:"Format"`
	ID        string          `json:"ID"`
	WrappedIn string          `json:"WrappedIn"`
}

type releaseArtifact struct {
	Name         string               `json:"name"`
	Path         string               `json:"path"`
	GOOS         string               `json:"goos"`
	GOARCH       string               `json:"goarch"`
	GOAMD64      string               `json:"goamd64"`
	GO386        string               `json:"go386"`
	GOARM        string               `json:"goarm"`
	GOARM64      string               `json:"goarm64"`
	GOMIPS       string               `json:"gomips"`
	GOPPC64      string               `json:"goppc64"`
	GORISCV64    string               `json:"goriscv64"`
	Target       string               `json:"target"`
	InternalType int                  `json:"internal_type"`
	Type         string               `json:"type"`
	Extra        releaseArtifactExtra `json:"extra"`
}

type releaseMetadataRuntime struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

type releaseMetadata struct {
	ProjectName string                 `json:"project_name"`
	Tag         string                 `json:"tag"`
	PreviousTag string                 `json:"previous_tag"`
	Version     string                 `json:"version"`
	Commit      string                 `json:"commit"`
	Date        string                 `json:"date"`
	Runtime     releaseMetadataRuntime `json:"runtime"`
}

type releaseManifest struct {
	Dir       string
	Artifacts []releaseArtifact
	Metadata  releaseMetadata
}

type releaseVersion struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Built   string `json:"built"`
}

type releaseSession struct {
	SchemaVersion int    `json:"schema_version"`
	SessionID     string `json:"session_id"`
	Harness       string `json:"harness"`
}

type archiveMember struct {
	Mode fs.FileMode
	Data []byte
}

func TestReleaseArtifacts(t *testing.T) {
	artifactDir := os.Getenv(artifactDirEnv)
	if artifactDir == "" {
		t.Skipf("%s is not set", artifactDirEnv)
	}

	manifest, err := loadArtifactManifest(artifactDir)
	if err != nil {
		t.Fatalf("load artifact manifest: %v", err)
	}
	if err := validateArtifactInventory(manifest); err != nil {
		t.Fatalf("validate artifact inventory: %v", err)
	}
	if err := validateChecksums(manifest); err != nil {
		t.Fatalf("validate checksums: %v", err)
	}
	if runtime.GOOS == "linux" {
		if err := validateLinuxPackages(manifest); err != nil {
			t.Fatalf("validate Linux packages: %v", err)
		}
	}
	if err := validateNativeArchive(manifest, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("validate native archive: %v", err)
	}

	publishedDir := os.Getenv(publishedArtifactDirEnv)
	if publishedDir != "" {
		if err := validatePublishedAssets(manifest, publishedDir); err != nil {
			t.Fatalf("validate published assets: %v", err)
		}
	}
	if os.Getenv(artifactNegativeControlsEnv) == "true" {
		if err := validateNegativeControls(manifest); err != nil {
			t.Fatalf("validate negative controls: %v", err)
		}
	}
}

func loadArtifactManifest(dir string) (releaseManifest, error) {
	artifactDir, err := resolveArtifactDir(dir)
	if err != nil {
		return releaseManifest{}, err
	}

	manifest := releaseManifest{Dir: artifactDir}
	if err := decodeJSONFile(filepath.Join(manifest.Dir, "artifacts.json"), &manifest.Artifacts); err != nil {
		return releaseManifest{}, fmt.Errorf("decode artifacts.json: %w", err)
	}
	if err := decodeJSONFile(filepath.Join(manifest.Dir, "metadata.json"), &manifest.Metadata); err != nil {
		return releaseManifest{}, fmt.Errorf("decode metadata.json: %w", err)
	}
	return manifest, nil
}

func resolveArtifactDir(dir string) (string, error) {
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir), nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve artifact directory: %w", err)
	}
	for candidateRoot := workingDir; ; candidateRoot = filepath.Dir(candidateRoot) {
		candidate := filepath.Join(candidateRoot, dir)
		if _, err := os.Stat(filepath.Join(candidate, "artifacts.json")); err == nil {
			return filepath.Clean(candidate), nil
		}
		parent := filepath.Dir(candidateRoot)
		if parent == candidateRoot {
			break
		}
	}
	return filepath.Clean(filepath.Join(workingDir, dir)), nil
}

func decodeJSONFile(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: trailing JSON content", filepath.Base(path))
	}
	return nil
}

func validateArtifactInventory(manifest releaseManifest) error {
	if manifest.Metadata.ProjectName != "aht" {
		return fmt.Errorf("metadata project_name = %q, want aht", manifest.Metadata.ProjectName)
	}
	if manifest.Metadata.Version == "" || manifest.Metadata.Commit == "" || manifest.Metadata.Date == "" {
		return fmt.Errorf("metadata version, commit, and date must be nonempty")
	}

	expectedPlatforms := map[string]struct{}{
		"linux/amd64":  {},
		"linux/arm64":  {},
		"darwin/amd64": {},
		"darwin/arm64": {},
	}
	expectedPackages := map[string]struct{}{
		"linux/amd64/deb": {},
		"linux/amd64/rpm": {},
		"linux/arm64/deb": {},
		"linux/arm64/rpm": {},
	}
	binaryPlatforms := make(map[string]struct{}, len(expectedPlatforms))
	archivePlatforms := make(map[string]struct{}, len(expectedPlatforms))
	packageTargets := make(map[string]struct{}, len(expectedPackages))
	downloadNames := make(map[string]string, len(manifest.Artifacts))
	metadataCount := 0
	checksumCount := 0

	for _, artifact := range manifest.Artifacts {
		path, err := artifactDiskPath(manifest, artifact)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			return missingArtifactError(artifact, err)
		}

		switch artifact.Type {
		case "Binary":
			if err := addUniqueTarget(binaryPlatforms, artifactPlatform(artifact), "Binary"); err != nil {
				return err
			}
		case "Archive":
			if artifact.Extra.Format != "tar.gz" {
				return fmt.Errorf("archive %q format = %q, want tar.gz", artifact.Name, artifact.Extra.Format)
			}
			if err := addUniqueTarget(archivePlatforms, artifactPlatform(artifact), "Archive"); err != nil {
				return err
			}
			if err := addUniqueDownload(downloadNames, artifact); err != nil {
				return err
			}
		case "Linux Package":
			format, err := linuxPackageFormat(artifact)
			if err != nil {
				return err
			}
			key := artifactPlatform(artifact) + "/" + format
			if err := addUniqueTarget(packageTargets, key, "Linux Package"); err != nil {
				return err
			}
			if err := addUniqueDownload(downloadNames, artifact); err != nil {
				return err
			}
		case "Checksum":
			checksumCount++
			if artifact.Name != "checksums.txt" {
				return fmt.Errorf("checksum artifact name = %q, want checksums.txt", artifact.Name)
			}
			if err := addUniqueDownload(downloadNames, artifact); err != nil {
				return err
			}
		case "Metadata":
			metadataCount++
			if artifact.Name != "metadata.json" {
				return fmt.Errorf("metadata artifact name = %q, want metadata.json", artifact.Name)
			}
		case "":
			return fmt.Errorf("artifact %q has no type", artifact.Name)
		default:
			return fmt.Errorf("unknown shipped artifact type %q for %q", artifact.Type, artifact.Name)
		}
	}

	if err := requireTargetSet("Binary", binaryPlatforms, expectedPlatforms); err != nil {
		return err
	}
	if err := requireTargetSet("Archive", archivePlatforms, expectedPlatforms); err != nil {
		return err
	}
	if err := requireTargetSet("Linux Package", packageTargets, expectedPackages); err != nil {
		return err
	}
	if checksumCount != 1 {
		return fmt.Errorf("Checksum artifact count = %d, want 1", checksumCount)
	}
	if metadataCount != 1 {
		return fmt.Errorf("Metadata artifact count = %d, want 1", metadataCount)
	}
	return nil
}

func artifactDiskPath(manifest releaseManifest, artifact releaseArtifact) (string, error) {
	if artifact.Name == "" || artifact.Name != filepath.Base(artifact.Name) || filepath.IsAbs(artifact.Name) {
		return "", fmt.Errorf("artifact name %q is not a basename", artifact.Name)
	}
	if artifact.Path == "" || filepath.IsAbs(filepath.FromSlash(artifact.Path)) {
		return "", fmt.Errorf("artifact %q has invalid manifest path %q", artifact.Name, artifact.Path)
	}

	manifestPath := filepath.Clean(filepath.FromSlash(artifact.Path))
	if manifestPath == ".." || strings.HasPrefix(manifestPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact %q path traverses outside artifact directory: %q", artifact.Name, artifact.Path)
	}
	parts := strings.Split(filepath.ToSlash(manifestPath), "/")
	if len(parts) > 1 && parts[0] == filepath.Base(manifest.Dir) {
		parts = parts[1:]
	}
	if len(parts) == 0 || parts[len(parts)-1] != artifact.Name {
		return "", fmt.Errorf("artifact %q path basename does not match manifest path %q", artifact.Name, artifact.Path)
	}

	relativePath := artifact.Name
	if artifact.Type == "Binary" {
		relativePath = filepath.FromSlash(strings.Join(parts, "/"))
	}
	path := filepath.Join(manifest.Dir, relativePath)
	if err := requirePathWithin(manifest.Dir, path); err != nil {
		return "", fmt.Errorf("artifact %q: %w", artifact.Name, err)
	}
	return path, nil
}

func requirePathWithin(root string, path string) error {
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("resolve relative path: %w", err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
		return fmt.Errorf("path %q escapes %q", path, root)
	}
	return nil
}

func missingArtifactError(artifact releaseArtifact, err error) error {
	if artifact.Type == "Archive" {
		return fmt.Errorf("missing %s/%s native archive %q: %w", artifact.GOOS, artifact.GOARCH, artifact.Name, err)
	}
	return fmt.Errorf("missing artifact file %q: %w", artifact.Name, err)
}

func artifactPlatform(artifact releaseArtifact) string {
	return artifact.GOOS + "/" + artifact.GOARCH
}

func addUniqueTarget(targets map[string]struct{}, key string, artifactType string) error {
	if _, exists := targets[key]; exists {
		return fmt.Errorf("duplicate %s OS/architecture/format tuple %q", artifactType, key)
	}
	targets[key] = struct{}{}
	return nil
}

func addUniqueDownload(names map[string]string, artifact releaseArtifact) error {
	if artifactType, exists := names[artifact.Name]; exists {
		return fmt.Errorf("duplicate downloadable artifact name %q for %s and %s", artifact.Name, artifactType, artifact.Type)
	}
	names[artifact.Name] = artifact.Type
	return nil
}

func linuxPackageFormat(artifact releaseArtifact) (string, error) {
	format := artifact.Extra.Format
	if format != "deb" && format != "rpm" {
		return "", fmt.Errorf("Linux package %q format = %q, want deb or rpm", artifact.Name, format)
	}
	extension := "." + format
	if artifact.Extra.Ext != extension || filepath.Ext(artifact.Name) != extension {
		return "", fmt.Errorf("Linux package %q extension does not match format %q", artifact.Name, format)
	}
	return format, nil
}

func requireTargetSet(kind string, got map[string]struct{}, want map[string]struct{}) error {
	missing := setDifference(want, got)
	extra := setDifference(got, want)
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	return fmt.Errorf("%s target set mismatch: missing=%v extra=%v", kind, missing, extra)
}

func setDifference(left map[string]struct{}, right map[string]struct{}) []string {
	difference := make([]string, 0)
	for value := range left {
		if _, exists := right[value]; !exists {
			difference = append(difference, value)
		}
	}
	sort.Strings(difference)
	return difference
}

func validateChecksums(manifest releaseManifest) error {
	expected := make(map[string]releaseArtifact)
	var checksumArtifact releaseArtifact
	for _, artifact := range manifest.Artifacts {
		switch artifact.Type {
		case "Archive", "Linux Package":
			expected[artifact.Name] = artifact
		case "Checksum":
			checksumArtifact = artifact
		}
	}
	if checksumArtifact.Name == "" {
		return fmt.Errorf("checksums.txt artifact is missing")
	}

	checksumPath, err := artifactDiskPath(manifest, checksumArtifact)
	if err != nil {
		return err
	}
	checksums, err := parseChecksums(checksumPath)
	if err != nil {
		return err
	}
	missing := make([]string, 0)
	extra := make([]string, 0)
	for name := range expected {
		if _, exists := checksums[name]; !exists {
			missing = append(missing, name)
		}
	}
	for name := range checksums {
		if _, exists := expected[name]; !exists {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) != 0 || len(extra) != 0 {
		return fmt.Errorf("checksum inventory mismatch: missing=%v extra=%v", missing, extra)
	}

	for name, artifact := range expected {
		path, err := artifactDiskPath(manifest, artifact)
		if err != nil {
			return err
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return fmt.Errorf("hash %q: %w", name, err)
		}
		if !bytes.Equal(digest, checksums[name]) {
			return fmt.Errorf("SHA-256 mismatch for %q: got %s want %s", name, hex.EncodeToString(digest), hex.EncodeToString(checksums[name]))
		}
	}
	return nil
}

func parseChecksums(path string) (map[string][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open checksums.txt: %w", err)
	}
	defer file.Close()

	checksums := make(map[string][]byte)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid checksums.txt line %q", scanner.Text())
		}
		name := fields[1]
		if name != filepath.Base(name) || filepath.IsAbs(name) {
			return nil, fmt.Errorf("checksum name %q is not a basename", name)
		}
		if _, exists := checksums[name]; exists {
			return nil, fmt.Errorf("duplicate checksum entry for %q", name)
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("invalid SHA-256 digest for %q", name)
		}
		checksums[name] = digest
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums.txt: %w", err)
	}
	return checksums, nil
}

func fileSHA256(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}

func validateNativeArchive(manifest releaseManifest, goos string, goarch string) error {
	artifact, err := findArtifact(manifest, "Archive", goos, goarch, "")
	if err != nil {
		return fmt.Errorf("missing native archive: %w", err)
	}
	archivePath, err := artifactDiskPath(manifest, artifact)
	if err != nil {
		return err
	}
	members, err := loadNativeArchive(archivePath)
	if err != nil {
		return err
	}

	extractDir, err := os.MkdirTemp("", "aht release artifacts ")
	if err != nil {
		return fmt.Errorf("create extraction directory: %w", err)
	}
	defer os.RemoveAll(extractDir)
	if !strings.Contains(extractDir, " ") {
		return fmt.Errorf("extraction directory %q does not contain a space", extractDir)
	}
	for name, member := range members {
		path := filepath.Join(extractDir, name)
		if err := os.WriteFile(path, member.Data, member.Mode.Perm()); err != nil {
			return fmt.Errorf("extract %q: %w", name, err)
		}
	}

	binary := filepath.Join(extractDir, "aht")
	return smokeReleaseBinary(binary, manifest.Metadata)
}

func loadNativeArchive(path string) (map[string]archiveMember, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open native archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("open native archive gzip stream: %w", err)
	}
	defer gzipReader.Close()

	expected := map[string]struct{}{"aht": {}, "LICENSE": {}, "README.md": {}}
	members := make(map[string]archiveMember, len(expected))
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read native archive: %w", err)
		}
		name := filepath.ToSlash(filepath.Clean(header.Name))
		if name != header.Name || name == "." || strings.Contains(name, "/") || filepath.IsAbs(header.Name) {
			return nil, fmt.Errorf("native archive member %q is not a safe basename", header.Name)
		}
		if _, exists := expected[name]; !exists {
			return nil, fmt.Errorf("unexpected native archive member %q", name)
		}
		if _, exists := members[name]; exists {
			return nil, fmt.Errorf("duplicate native archive member %q", name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("native archive member %q is not a regular file", name)
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, fmt.Errorf("read native archive member %q: %w", name, err)
		}
		members[name] = archiveMember{Mode: fs.FileMode(header.Mode), Data: data}
	}

	missing := setDifference(expected, memberNames(members))
	if len(missing) != 0 {
		return nil, fmt.Errorf("missing native archive members %v", missing)
	}
	if members["aht"].Mode.Perm()&0o111 == 0 {
		return nil, fmt.Errorf("native archive aht member is not executable: mode %04o", members["aht"].Mode.Perm())
	}
	return members, nil
}

func memberNames(members map[string]archiveMember) map[string]struct{} {
	names := make(map[string]struct{}, len(members))
	for name := range members {
		names[name] = struct{}{}
	}
	return names
}

func smokeReleaseBinary(binary string, metadata releaseMetadata) error {
	isolatedRoot, err := os.MkdirTemp("", "aht release state ")
	if err != nil {
		return fmt.Errorf("create isolated state directory: %w", err)
	}
	defer os.RemoveAll(isolatedRoot)
	workingDir, err := os.MkdirTemp("", "aht release work ")
	if err != nil {
		return fmt.Errorf("create working directory: %w", err)
	}
	defer os.RemoveAll(workingDir)

	home := filepath.Join(isolatedRoot, "home")
	configHome := filepath.Join(isolatedRoot, "config")
	stateDir := filepath.Join(isolatedRoot, "state")
	for _, dir := range []string{home, configHome, stateDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create isolated directory %q: %w", dir, err)
		}
	}
	environment := isolatedEnvironment(home, configHome, stateDir)

	versionOutput, err := runReleaseCommand(binary, workingDir, environment, "--json", "--version")
	if err != nil {
		return err
	}
	var version releaseVersion
	if err := json.Unmarshal(versionOutput, &version); err != nil {
		return fmt.Errorf("decode version output %q: %w", versionOutput, err)
	}
	if version.Version != metadata.Version {
		return fmt.Errorf("version mismatch: binary=%q metadata=%q", version.Version, metadata.Version)
	}
	if version.Commit == "" || !strings.HasPrefix(metadata.Commit, version.Commit) {
		return fmt.Errorf("commit mismatch: binary=%q metadata=%q", version.Commit, metadata.Commit)
	}
	if !releaseDatesEqual(version.Built, metadata.Date) {
		return fmt.Errorf("build date mismatch: binary=%q metadata=%q", version.Built, metadata.Date)
	}

	helpOutput, err := runReleaseCommand(binary, workingDir, environment, "--help")
	if err != nil {
		return err
	}
	help := string(helpOutput)
	if !strings.Contains(help, "aht") || !strings.Contains(help, "Available Commands:") {
		return fmt.Errorf("help output does not identify aht and its available commands: %q", help)
	}

	storePath := filepath.Join(isolatedRoot, "state.json")
	reportOutput, err := runReleaseCommand(binary, workingDir, environment, "--store", storePath, "--json", "report", "codex", "--session-id", "release-smoke", "--event", "start", "--no-tmux")
	if err != nil {
		return err
	}
	var reported releaseSession
	if err := json.Unmarshal(reportOutput, &reported); err != nil {
		return fmt.Errorf("decode report output %q: %w", reportOutput, err)
	}
	if reported.SchemaVersion != 2 || reported.SessionID != "release-smoke" || reported.Harness != "codex" {
		return fmt.Errorf("report output does not contain the schema-v2 Codex session: %q", reportOutput)
	}

	listOutput, err := runReleaseCommand(binary, workingDir, environment, "--store", storePath, "--json", "list")
	if err != nil {
		return err
	}
	var sessions []releaseSession
	if err := json.Unmarshal(listOutput, &sessions); err != nil {
		return fmt.Errorf("decode list output %q: %w", listOutput, err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != reported.SessionID || sessions[0].Harness != reported.Harness {
		return fmt.Errorf("list output does not contain exactly the reported session: %q", listOutput)
	}
	return nil
}

func isolatedEnvironment(home string, configHome string, stateDir string) []string {
	replacements := map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": configHome,

		"AHT_STATE_DIR":   stateDir,
	}
	environment := make([]string, 0, len(os.Environ())+len(replacements))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if _, replaced := replacements[key]; found && replaced {
			continue
		}
		environment = append(environment, entry)
	}
	for key, value := range replacements {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func releaseDatesEqual(binaryDate string, metadataDate string) bool {
	binaryTime, err := time.Parse(time.RFC3339Nano, binaryDate)
	if err != nil {
		return false
	}
	metadataTime, err := time.Parse(time.RFC3339Nano, metadataDate)
	if err != nil {
		return false
	}
	return binaryTime.Equal(metadataTime.Truncate(time.Second))
}

func runReleaseCommand(binary string, dir string, environment []string, args ...string) ([]byte, error) {
	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Env = environment
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("run %q %q: %w; stdout=%q stderr=%q", binary, args, err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		return nil, fmt.Errorf("run %q %q wrote stderr=%q", binary, args, stderr.String())
	}
	return stdout.Bytes(), nil
}

func validateLinuxPackages(manifest releaseManifest) error {
	dpkgDeb, err := exec.LookPath("dpkg-deb")
	if err != nil {
		return fmt.Errorf("dpkg-deb is required to inspect release .deb packages; install dpkg: %w", err)
	}
	rpm, err := exec.LookPath("rpm")
	if err != nil {
		return fmt.Errorf("rpm is required to inspect release .rpm packages; install rpm: %w", err)
	}

	for _, artifact := range manifest.Artifacts {
		if artifact.Type != "Linux Package" {
			continue
		}
		path, err := artifactDiskPath(manifest, artifact)
		if err != nil {
			return err
		}
		format, err := linuxPackageFormat(artifact)
		if err != nil {
			return err
		}
		if format == "deb" {
			if err := validateDebPackage(dpkgDeb, path, artifact.GOARCH); err != nil {
				return fmt.Errorf("validate %q: %w", artifact.Name, err)
			}
			continue
		}
		if err := validateRPMPackage(rpm, path, artifact.GOARCH); err != nil {
			return fmt.Errorf("validate %q: %w", artifact.Name, err)
		}
	}
	return nil
}

func validateDebPackage(dpkgDeb string, path string, goarch string) error {
	packageName, err := runInspectionCommand(dpkgDeb, "--field", path, "Package")
	if err != nil {
		return err
	}
	if strings.TrimSpace(packageName) != "aht" {
		return fmt.Errorf("package name = %q, want aht", strings.TrimSpace(packageName))
	}
	architecture, err := runInspectionCommand(dpkgDeb, "--field", path, "Architecture")
	if err != nil {
		return err
	}
	if strings.TrimSpace(architecture) != goarch {
		return fmt.Errorf("package architecture = %q, want %q", strings.TrimSpace(architecture), goarch)
	}
	contents, err := runInspectionCommand(dpkgDeb, "--contents", path)
	if err != nil {
		return err
	}
	if !packageContentsPath(contents, "usr/bin/aht") {
		return fmt.Errorf("package does not contain /usr/bin/aht: %q", contents)
	}
	return nil
}

func validateRPMPackage(rpm string, path string, goarch string) error {
	metadata, err := runInspectionCommand(rpm, "-qp", "--queryformat", "%{NAME}\n%{ARCH}\n", path)
	if err != nil {
		return err
	}
	fields := strings.Fields(metadata)
	if len(fields) != 2 || fields[0] != "aht" {
		return fmt.Errorf("RPM metadata = %q, want package aht and one architecture", metadata)
	}
	expectedArchitecture := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if fields[1] != expectedArchitecture {
		return fmt.Errorf("RPM architecture = %q, want %q", fields[1], expectedArchitecture)
	}
	contents, err := runInspectionCommand(rpm, "-qlp", path)
	if err != nil {
		return err
	}
	if !packageContentsPath(contents, "usr/bin/aht") {
		return fmt.Errorf("package does not contain /usr/bin/aht: %q", contents)
	}
	return nil
}

func packageContentsPath(output string, wanted string) bool {
	for line := range strings.Lines(output) {
		path := strings.TrimSpace(line)
		fields := strings.Fields(path)
		if len(fields) != 0 {
			path = fields[len(fields)-1]
		}
		path = strings.TrimPrefix(path, "./")
		path = strings.TrimPrefix(path, "/")
		if path == wanted {
			return true
		}
	}
	return false
}

func runInspectionCommand(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %q %q: %w; output=%q", name, args, err, output)
	}
	return string(output), nil
}

func validatePublishedAssets(manifest releaseManifest, publishedDir string) error {
	expected := make(map[string]releaseArtifact)
	for _, artifact := range manifest.Artifacts {
		if artifact.Type == "Archive" || artifact.Type == "Linux Package" || artifact.Type == "Checksum" {
			expected[artifact.Name] = artifact
		}
	}

	entries, err := os.ReadDir(publishedDir)
	if err != nil {
		return fmt.Errorf("read published artifact directory: %w", err)
	}
	actual := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return fmt.Errorf("published asset %q is not a regular file", entry.Name())
		}
		actual[entry.Name()] = struct{}{}
	}
	expectedNames := make(map[string]struct{}, len(expected))
	for name := range expected {
		expectedNames[name] = struct{}{}
	}
	if err := requireTargetSet("published asset", actual, expectedNames); err != nil {
		return err
	}

	for name, artifact := range expected {
		distPath, err := artifactDiskPath(manifest, artifact)
		if err != nil {
			return err
		}
		equal, err := filesEqual(distPath, filepath.Join(publishedDir, name))
		if err != nil {
			return fmt.Errorf("compare published asset %q: %w", name, err)
		}
		if !equal {
			return fmt.Errorf("published asset %q differs from tested dist copy", name)
		}
	}
	return nil
}

func filesEqual(leftPath string, rightPath string) (bool, error) {
	left, err := os.Open(leftPath)
	if err != nil {
		return false, err
	}
	defer left.Close()
	right, err := os.Open(rightPath)
	if err != nil {
		return false, err
	}
	defer right.Close()

	leftBuffer := make([]byte, 32*1024)
	rightBuffer := make([]byte, len(leftBuffer))
	for {
		leftCount, leftErr := io.ReadFull(left, leftBuffer)
		rightCount, rightErr := io.ReadFull(right, rightBuffer)
		if leftCount != rightCount || !bytes.Equal(leftBuffer[:leftCount], rightBuffer[:rightCount]) {
			return false, nil
		}
		if errors.Is(leftErr, io.EOF) && errors.Is(rightErr, io.EOF) {
			return true, nil
		}
		if errors.Is(leftErr, io.ErrUnexpectedEOF) && errors.Is(rightErr, io.ErrUnexpectedEOF) {
			return true, nil
		}
		if leftErr != nil && !errors.Is(leftErr, io.ErrUnexpectedEOF) {
			return false, leftErr
		}
		if rightErr != nil && !errors.Is(rightErr, io.ErrUnexpectedEOF) {
			return false, rightErr
		}
	}
}

func findArtifact(manifest releaseManifest, artifactType string, goos string, goarch string, format string) (releaseArtifact, error) {
	var matches []releaseArtifact
	for _, artifact := range manifest.Artifacts {
		if artifact.Type != artifactType || artifact.GOOS != goos || artifact.GOARCH != goarch {
			continue
		}
		if format != "" && artifact.Extra.Format != format {
			continue
		}
		matches = append(matches, artifact)
	}
	if len(matches) != 1 {
		return releaseArtifact{}, fmt.Errorf("found %d %s artifacts for %s/%s format %q, want 1", len(matches), artifactType, goos, goarch, format)
	}
	return matches[0], nil
}

func validateNegativeControls(manifest releaseManifest) error {
	if err := validateMissingArchiveControl(manifest); err != nil {
		return err
	}
	if err := validateArchiveModeControl(manifest); err != nil {
		return err
	}
	if err := validateMetadataVersionControl(manifest); err != nil {
		return err
	}
	return nil
}

func validateMissingArchiveControl(manifest releaseManifest) error {
	mutated, cleanup, err := copyReleaseManifest(manifest)
	if err != nil {
		return err
	}
	defer cleanup()
	archive, err := findArtifact(mutated, "Archive", runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		return err
	}
	path, err := artifactDiskPath(mutated, archive)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove negative-control archive: %w", err)
	}
	return requireValidationFailure(validateArtifactInventory(mutated), "missing "+runtime.GOOS+"/"+runtime.GOARCH+" native archive", "missing archive negative control")
}

func validateArchiveModeControl(manifest releaseManifest) error {
	mutated, cleanup, err := copyReleaseManifest(manifest)
	if err != nil {
		return err
	}
	defer cleanup()
	archive, err := findArtifact(mutated, "Archive", runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		return err
	}
	path, err := artifactDiskPath(mutated, archive)
	if err != nil {
		return err
	}
	if err := rewriteArchiveBinaryMode(path, 0o644); err != nil {
		return err
	}
	if err := updateChecksum(mutated, archive.Name); err != nil {
		return err
	}
	if err := validateChecksums(mutated); err != nil {
		return fmt.Errorf("mode negative control did not reach archive oracle: %w", err)
	}
	return requireValidationFailure(validateNativeArchive(mutated, runtime.GOOS, runtime.GOARCH), "not executable", "archive mode negative control")
}

func validateMetadataVersionControl(manifest releaseManifest) error {
	mutated, cleanup, err := copyReleaseManifest(manifest)
	if err != nil {
		return err
	}
	defer cleanup()
	mutated.Metadata.Version += "-negative-control"
	data, err := json.Marshal(mutated.Metadata)
	if err != nil {
		return fmt.Errorf("encode negative-control metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(mutated.Dir, "metadata.json"), data, 0o644); err != nil {
		return fmt.Errorf("write negative-control metadata: %w", err)
	}
	reloaded, err := loadArtifactManifest(mutated.Dir)
	if err != nil {
		return err
	}
	return requireValidationFailure(validateNativeArchive(reloaded, runtime.GOOS, runtime.GOARCH), "version mismatch", "metadata version negative control")
}

func requireValidationFailure(err error, category string, control string) error {
	if err == nil {
		return fmt.Errorf("%s unexpectedly passed", control)
	}
	if !strings.Contains(err.Error(), category) {
		return fmt.Errorf("%s failed for unrelated reason: got %q, want category %q", control, err, category)
	}
	return nil
}

func copyReleaseManifest(manifest releaseManifest) (releaseManifest, func(), error) {
	parent, err := os.MkdirTemp("", "aht artifact control ")
	if err != nil {
		return releaseManifest{}, func() {}, fmt.Errorf("create negative-control directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(parent) }
	destination := filepath.Join(parent, filepath.Base(manifest.Dir))
	if err := copyDirectory(manifest.Dir, destination); err != nil {
		cleanup()
		return releaseManifest{}, func() {}, err
	}
	copied, err := loadArtifactManifest(destination)
	if err != nil {
		cleanup()
		return releaseManifest{}, func() {}, err
	}
	return copied, cleanup, nil
}

func copyDirectory(source string, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("resolve copy path: %w", err)
		}
		target := filepath.Join(destination, relativePath)
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat copy source %q: %w", path, err)
		}
		if entry.IsDir() {
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create copy directory %q: %w", target, err)
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("cannot copy non-regular artifact path %q", path)
		}
		if err := copyFile(path, target, info.Mode().Perm()); err != nil {
			return err
		}
		return nil
	})
}

func copyFile(source string, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open copy source %q: %w", source, err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create copy destination %q: %w", destination, err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return fmt.Errorf("copy %q: %w", source, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close copy destination %q: %w", destination, err)
	}
	return nil
}

func rewriteArchiveBinaryMode(path string, mode int64) error {
	input, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open archive for mutation: %w", err)
	}
	defer input.Close()
	gzipReader, err := gzip.NewReader(input)
	if err != nil {
		return fmt.Errorf("open archive gzip for mutation: %w", err)
	}
	defer gzipReader.Close()

	temporaryPath := path + ".negative-control"
	output, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create mutated archive: %w", err)
	}
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	tarReader := tar.NewReader(gzipReader)
	mutated := false
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return closeMutatedArchive(output, gzipWriter, tarWriter, temporaryPath, fmt.Errorf("read archive for mutation: %w", err))
		}
		headerCopy := *header
		if headerCopy.Name == "aht" {
			headerCopy.Mode = mode
			mutated = true
		}
		if err := tarWriter.WriteHeader(&headerCopy); err != nil {
			return closeMutatedArchive(output, gzipWriter, tarWriter, temporaryPath, fmt.Errorf("write mutated archive header: %w", err))
		}
		if _, err := io.Copy(tarWriter, tarReader); err != nil {
			return closeMutatedArchive(output, gzipWriter, tarWriter, temporaryPath, fmt.Errorf("write mutated archive content: %w", err))
		}
	}
	if !mutated {
		return closeMutatedArchive(output, gzipWriter, tarWriter, temporaryPath, fmt.Errorf("archive mutation did not find aht"))
	}
	if err := tarWriter.Close(); err != nil {
		return closeMutatedArchive(output, gzipWriter, nil, temporaryPath, err)
	}
	if err := gzipWriter.Close(); err != nil {
		return closeMutatedArchive(output, nil, nil, temporaryPath, err)
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close mutated archive: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace archive with mutation: %w", err)
	}
	return nil
}

func closeMutatedArchive(output *os.File, gzipWriter *gzip.Writer, tarWriter *tar.Writer, temporaryPath string, cause error) error {
	if tarWriter != nil {
		_ = tarWriter.Close()
	}
	if gzipWriter != nil {
		_ = gzipWriter.Close()
	}
	_ = output.Close()
	_ = os.Remove(temporaryPath)
	return cause
}

func updateChecksum(manifest releaseManifest, artifactName string) error {
	artifact, err := findArtifactByName(manifest, artifactName)
	if err != nil {
		return err
	}
	artifactPath, err := artifactDiskPath(manifest, artifact)
	if err != nil {
		return err
	}
	digest, err := fileSHA256(artifactPath)
	if err != nil {
		return err
	}
	checksumArtifact, err := findArtifactByName(manifest, "checksums.txt")
	if err != nil {
		return err
	}
	checksumPath, err := artifactDiskPath(manifest, checksumArtifact)
	if err != nil {
		return err
	}
	checksums, err := parseChecksums(checksumPath)
	if err != nil {
		return err
	}
	checksums[artifactName] = digest
	names := make([]string, 0, len(checksums))
	for name := range checksums {
		names = append(names, name)
	}
	sort.Strings(names)
	var content strings.Builder
	for _, name := range names {
		fmt.Fprintf(&content, "%s  %s\n", hex.EncodeToString(checksums[name]), name)
	}
	if err := os.WriteFile(checksumPath, []byte(content.String()), 0o644); err != nil {
		return fmt.Errorf("write updated checksums.txt: %w", err)
	}
	return nil
}

func findArtifactByName(manifest releaseManifest, name string) (releaseArtifact, error) {
	var matches []releaseArtifact
	for _, artifact := range manifest.Artifacts {
		if artifact.Name == name {
			matches = append(matches, artifact)
		}
	}
	if len(matches) != 1 {
		return releaseArtifact{}, fmt.Errorf("found %d artifacts named %q, want 1", len(matches), name)
	}
	return matches[0], nil
}
