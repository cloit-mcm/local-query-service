package mysql

import (
	"fmt"
	"github.com/pkg/errors"
	"strings"

	"github.com/jmoiron/sqlx"
)

func InitDB(db *sqlx.DB) error {
	var err error
	if db == nil {
		return fmt.Errorf("invalid db connection")
	}

	table_schema := `CREATE TABLE IF NOT EXISTS agent_config_versions(
		id VARCHAR(50) PRIMARY KEY,
		created_by varchar(50),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_by varchar(50),
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		version INTEGER DEFAULT 1,
		active int,
		is_valid int,
		disabled int,
		element_type VARCHAR(120) NOT NULL,
		deploy_status VARCHAR(80) NOT NULL DEFAULT 'DIRTY',
		deploy_sequence INTEGER,
		deploy_result TEXT,
		last_hash varchar(100),
		last_config varchar(100),
		UNIQUE INDEX agent_config_versions_u1(element_type, version),
		INDEX agent_config_versions_nu1(last_hash)
		);
	
||
	CREATE TABLE IF NOT EXISTS agent_config_elements(
		id VARCHAR(50) PRIMARY KEY,
		created_by varchar(50),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_by varchar(50),
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		element_id VARCHAR(50) NOT NULL,
		element_type VARCHAR(120) NOT NULL,
		version_id VARCHAR(50) NOT NULL,
		unique index agent_config_elements_u1 (version_id, element_id, element_type)
		);

	`

	table_schemaArray := strings.Split(table_schema, "||")
	for _, schema := range table_schemaArray {
		_, err = db.Exec(schema)
		if err != nil {
			return errors.Wrap(err, "Error in creating agent config tables")
		}
	}
	return nil
}
