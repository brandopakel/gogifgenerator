package cinematic

import (
	"os/exec"
	"strings"
)

// ProbeExecutable reports a local dependency without launching it. Detailed
// version checks happen in each stage constructor when the pipeline is enabled.
func ProbeExecutable(id, label, role, executable string) StageDescriptor {
	descriptor := StageDescriptor{ID: id, Label: label, Role: role, Local: true}
	_, err := exec.LookPath(strings.TrimSpace(executable))
	if err != nil {
		descriptor.Detail = "not installed or not on PATH"
		return descriptor
	}
	descriptor.Available = true
	return descriptor
}

func DisabledDescriptor(stages []StageDescriptor) Descriptor {
	return Descriptor{
		ID: "cinematic-local", Label: "Cinematic local pipeline", Local: true,
		Enabled: false, SupportsReferences: true, Stages: append([]StageDescriptor(nil), stages...),
	}
}
