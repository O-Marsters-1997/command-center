package store

import "database/sql"

func (s *Store) DB() *sql.DB { return s.db }
