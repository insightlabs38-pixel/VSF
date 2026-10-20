package meta

import (
	"fmt"
	"strings"
)

// PostgresScript renders the shipped PostgreSQL migrations as one psql script that skips applied steps and stops on drift.
func PostgresScript(upTo int) (string, error) {
	all, err := Migrations("postgres")
	if err != nil {
		return "", err
	}
	if upTo <= 0 || upTo > len(all) {
		upTo = len(all)
	}
	var b strings.Builder
	b.WriteString("\\set ON_ERROR_STOP on\n")
	b.WriteString("CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at BIGINT NOT NULL);\n")
	for _, m := range all[:upTo] {
		fmt.Fprintf(&b, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = %d) AS applied \\gset\n", m.Version)
		fmt.Fprintf(&b, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = %d AND checksum <> '%s') AS drifted \\gset\n", m.Version, m.Checksum)
		b.WriteString("\\if :drifted\n  \\warn migration history differs from the one shipped\n  SELECT 1/0;\n\\endif\n")
		b.WriteString("\\if :applied\n\\else\nBEGIN;\n")
		b.WriteString(m.SQL)
		if !strings.HasSuffix(m.SQL, "\n") {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (%d, '%s', '%s', (extract(epoch from now()) * 1000)::bigint);\nCOMMIT;\n\\endif\n", m.Version, m.Name, m.Checksum)
	}
	return b.String(), nil
}
