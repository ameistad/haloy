package db

func (db *DB) Migrate() error {
	deployment := Deployment{}
	if err := deployment.CreateTable(db); err != nil {
		return err
	}
	return nil
}
