// Package assets embeds prompt templates and .env.example into the binary,
// so a bare release works out of the box (files are extracted on first run).
package assets

import _ "embed"

//go:embed baseprompt_min.json
var MinTemplate []byte

//go:embed baseprompt.json
var FullTemplate []byte

//go:embed .env.example
var EnvExample []byte
