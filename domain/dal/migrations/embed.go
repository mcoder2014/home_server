// Package migrations exposes reviewed SQL to explicit offline migration commands.
package migrations

import "embed"

// SQL includes historical fixtures for isolated restore tests. Production callers
// must select the exact migration files they intend to execute.
//
//go:embed *.sql
var SQL embed.FS
