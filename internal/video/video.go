// Package video defines the small boundary between request handling and an
// optional local video decoder.
package video

import (
	"context"
	"image/gif"
)

// Request describes a bounded clip to decode into source frames. Data only
// lives for the duration of the request.
type Request struct {
	Data     []byte
	Filename string
	StartMS  int
	EndMS    int
	Frames   int
}

// Descriptor is safe to expose through the public capability endpoint.
type Descriptor struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Local bool   `json:"local"`
}

// Decoder converts a short local clip into a bounded GIF animation for the Go
// editor pipeline.
type Decoder interface {
	Descriptor() Descriptor
	Decode(context.Context, Request) (*gif.GIF, error)
}
