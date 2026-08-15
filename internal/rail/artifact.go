package rail

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func VerifyArtifacts(manifest Manifest, baseDir string) []ArtifactResult {
	components := append([]Component(nil), manifest.Components...)
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })
	results := make([]ArtifactResult, 0, len(components))
	for _, component := range components {
		results = append(results, VerifyArtifact(component.Name, component.Artifact, baseDir))
	}
	return results
}

func VerifyArtifact(component string, artifact Artifact, baseDir string) ArtifactResult {
	result := ArtifactResult{Component: component, Path: artifact.Path}
	if artifact.Path == "" {
		result.Error = "artifact path is empty"
		return result
	}
	if filepath.IsAbs(artifact.Path) {
		result.Error = "artifact path must be relative"
		return result
	}
	clean := filepath.Clean(artifact.Path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		result.Error = "artifact path escapes base directory"
		return result
	}
	baseAbsolute, err := filepath.Abs(baseDir)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	full := filepath.Join(baseAbsolute, clean)
	fullAbsolute, err := filepath.Abs(full)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	resolvedBase, err := filepath.EvalSymlinks(baseAbsolute)
	if err != nil {
		result.Error = fmt.Sprintf("resolve base directory: %v", err)
		return result
	}
	resolvedFull, err := filepath.EvalSymlinks(fullAbsolute)
	if err != nil {
		result.Error = fmt.Sprintf("resolve artifact: %v", err)
		return result
	}
	relative, err := filepath.Rel(resolvedBase, resolvedFull)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		result.Error = "artifact resolves outside base directory"
		return result
	}
	file, err := os.Open(resolvedFull)
	if err != nil {
		result.Error = fmt.Sprintf("open artifact: %v", err)
		return result
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		result.Error = fmt.Sprintf("stat artifact: %v", err)
		return result
	}
	if !info.Mode().IsRegular() {
		result.Error = "artifact is not a regular file"
		return result
	}
	hash := sha256.New()
	count, err := io.Copy(hash, file)
	if err != nil {
		result.Error = fmt.Sprintf("hash artifact: %v", err)
		return result
	}
	result.Size = count
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	expectedHash := strings.ToLower(artifact.SHA256)
	if count != artifact.Size {
		result.Error = fmt.Sprintf("size mismatch: expected %d, got %d", artifact.Size, count)
		return result
	}
	if result.SHA256 != expectedHash {
		result.Error = fmt.Sprintf("SHA-256 mismatch: expected %s, got %s", expectedHash, result.SHA256)
		return result
	}
	result.Valid = true
	return result
}

func ArtifactSummary(results []ArtifactResult) (int, int, []string) {
	valid := 0
	invalid := 0
	messages := []string{}
	for _, result := range results {
		if result.Valid {
			valid++
			continue
		}
		invalid++
		messages = append(messages, result.Component+": "+result.Error)
	}
	sort.Strings(messages)
	return valid, invalid, messages
}
