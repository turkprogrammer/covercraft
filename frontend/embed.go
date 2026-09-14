package frontend

import _ "embed"

// IndexHTML — единственный файл UI, зашитый в бинарник (KISS: без бандлеров).
//
//go:embed index.html
var IndexHTML string
