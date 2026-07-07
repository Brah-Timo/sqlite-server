package engine

// Translator is the public entry point for the full rewrite pipeline.
// It is a thin wrapper around planner.Planner that keeps the engine
// package self-contained.
//
// All the actual translation logic lives in:
//   sql/planner/normalizer.go  — structural normalisation
//   sql/planner/rewriter.go    — PG → SQLite semantic rewrites
//   sql/planner/planner.go     — AST → SQL emission
//
// This file exists so that callers only need to import engine/translator
// rather than traversing into the sql/ sub-package.

import (
	"github.com/sqlite-server/sqlite-server/sql/planner"
)

// Translator rewrites PostgreSQL SQL into SQLite SQL.
type Translator struct {
	p *planner.Planner
}

// NewTranslator constructs a Translator.
func NewTranslator() *Translator {
	return &Translator{p: planner.New()}
}

// Translate rewrites pgSQL to a SQLite-compatible SQL string.
// If parsing fails the original string is returned (best-effort passthrough).
func (t *Translator) Translate(pgSQL string) (string, error) {
	return t.p.Rewrite(pgSQL)
}

// TranslateOne translates a single statement.
func (t *Translator) TranslateOne(pgSQL string) (string, error) {
	return t.p.RewriteOne(pgSQL)
}
