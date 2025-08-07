package storage

func (db *DB) Migrate() error {
	if err := createDeploymentsTable(db); err != nil {
		return err
	}

	if err := createSecretsTable(db); err != nil {
		return err
	}
	return nil
}
