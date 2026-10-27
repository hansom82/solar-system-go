package main

import (
	"embed"
)

// Real planet textures from the Solar System Scope texture pack
// (https://www.solarsystemscope.com/textures/), licensed CC BY 4.0.
// They are equirectangular JPEGs (2048x1024) that map directly onto the
// GenMeshSphere UV layout.
//
// go:embed bakes every JPEG into the compiled binary at build time, so the
// executable is fully self-contained and needs no external asset files at
// runtime.
//
//go:embed textures/*.jpg
var textureFS embed.FS

// embeddedTexture returns the raw JPEG bytes for a named body (e.g. "earth"),
// or nil when no texture is bundled (the caller then falls back to a
// procedurally generated texture).
func embeddedTexture(name string) []byte {
	if name == "" {
		return nil
	}
	b, err := textureFS.ReadFile("textures/tex_" + name + ".jpg")
	if err != nil {
		return nil
	}
	return b
}
