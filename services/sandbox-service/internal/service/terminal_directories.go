package service

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode"

	core "k8s.io/api/core/v1"
)

const terminalDirectoriesAnnotation = "quenching.io/terminal-directories"

// The template declares directories per container; no project-specific path
// guesses live in the platform. Pod workingDir remains a legacy fallback.
func containerDirectories(pod *core.Pod, container core.Container) (ContainerTarget, error) {
	result := ContainerTarget{Name: container.Name, WorkingDir: container.WorkingDir}
	if raw := pod.Annotations[terminalDirectoriesAnnotation]; raw != "" {
		var directories map[string]ContainerTarget
		if err := json.Unmarshal([]byte(raw), &directories); err != nil {
			return result, fmt.Errorf("invalid terminal directories on %s: %w", pod.Name, err)
		}
		if declared, ok := directories[container.Name]; ok {
			if declared.WorkingDir != "" {
				result.WorkingDir = declared.WorkingDir
			}
			result.DeploymentDir, result.ConfigDir = declared.DeploymentDir, declared.ConfigDir
		}
	}
	for _, dir := range []string{result.WorkingDir, result.DeploymentDir, result.ConfigDir} {
		if dir != "" && (!path.IsAbs(dir) || path.Clean(dir) != dir || len(dir) > 4096 || strings.IndexFunc(dir, unicode.IsControl) >= 0) {
			return result, fmt.Errorf("invalid terminal directory for %s/%s", pod.Name, container.Name)
		}
	}
	return result, nil
}
