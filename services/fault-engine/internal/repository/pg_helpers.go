package repository

type scannable interface{ Scan(...any) error }

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
