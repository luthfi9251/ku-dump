package config

import "os"

type Config struct {
	Addr     string
	DBPath   string
	DumpsDir string
	CryptKey string
	Tools    map[string]string
}

func Load() Config {
	return Config{
		Addr:     getenv("KUDUMP_ADDR", ":8080"),
		DBPath:   getenv("KUDUMP_DB", "ku-dump.db"),
		DumpsDir: getenv("KUDUMP_DUMPS_DIR", "dumps"),
		CryptKey: os.Getenv("KUDUMP_CRYPT_KEY"),
		Tools: map[string]string{
			"pg_dump":      os.Getenv("KUDUMP_PG_DUMP"),
			"pg_restore":   os.Getenv("KUDUMP_PG_RESTORE"),
			"mongodump":    os.Getenv("KUDUMP_MONGODUMP"),
			"mongorestore": os.Getenv("KUDUMP_MONGORESTORE"),
		},
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
