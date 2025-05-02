package ui

import "embed"

//go:embed static/style.css
var StaticFS embed.FS

//go:embed static/favicon.svg
var FaviconSVG []byte
