package planner

import (
	"context"
	"hash/fnv"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/intent"
)

// Local is fast, free, offline, and deterministic for the same prompt and
// seed. It reads the prompt with the shared offline interpreter, so its plan
// follows the same subject, action, style, mood, and camera vocabulary a
// remote model would return.
type Local struct{}

func (Local) Plan(_ context.Context, request Request) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(request.Prompt))))
	seed := int64(h.Sum64())
	if request.Seed != 0 {
		seed ^= request.Seed
	}

	brief := intent.Interpret(request.Prompt)
	spec, err := specFrom(request, brief, seed)
	if err != nil {
		return Result{}, err
	}
	return Result{Spec: spec, Brief: brief, Engine: "local"}, nil
}
