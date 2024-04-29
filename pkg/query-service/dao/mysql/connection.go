package mysql

import (
	"context"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"go.signoz.io/signoz/pkg/query-service/constants"
	"go.signoz.io/signoz/pkg/query-service/model"
	"go.signoz.io/signoz/pkg/query-service/telemetry"
	"go.uber.org/zap"
	"strings"
)

type ModelDaoMysql struct {
	db *sqlx.DB
}

// InitDB sets up setting up the connection pool global variable.
func InitDB(dataSourceName string) (*ModelDaoMysql, error) {
	var err error

	db, err := sqlx.Open("mysql", "root:1q2w3e4r!@(localhost:3306)/signoz_meta?parseTime=true")
	if err != nil {
		return nil, errors.Wrap(err, "failed to Open mysql DB")
	}
	db.SetMaxOpenConns(10)
	//
	table_schema := `
CREATE TABLE IF NOT EXISTS agents (
agent_id VARCHAR(50) PRIMARY KEY,
started_at datetime NOT NULL,
terminated_at datetime,
current_status TEXT NOT NULL,
effective_config TEXT NOT NULL
);
||
-- apdex_settings definition
CREATE TABLE IF NOT EXISTS apdex_settings (
service_name varchar(100) PRIMARY KEY,
threshold FLOAT NOT NULL,
exclude_status_codes TEXT NOT NULL
);
||
-- feature_status definition
CREATE TABLE IF NOT EXISTS feature_status (
name varchar(100) PRIMARY KEY,
active bool,
usage_cnt INT DEFAULT 0,
usage_limit INTEGER DEFAULT 0,
route varchar(100)
);
||
-- groups definition
CREATE TABLE IF NOT EXISTS group_info (
id VARCHAR(50) PRIMARY KEY,
group_name varchar(100) NOT NULL UNIQUE
);
||
-- ingestion_keys definition
CREATE TABLE IF NOT EXISTS ingestion_keys (
key_id VARCHAR(50) PRIMARY KEY,
name varchar(100),
created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
ingestion_key varchar(500) NOT NULL,
ingestion_url varchar(500) NOT NULL,
data_region varchar(100)NOT NULL
);
||
-- integrations_installed definition
CREATE TABLE IF NOT EXISTS integrations_installed(
integration_id VARCHAR(50) PRIMARY KEY,
config_json TEXT,
installed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
||
-- licenses definition
CREATE TABLE IF NOT EXISTS licenses(
licenses_key varchar(100) PRIMARY KEY,
createdAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
updatedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
planDetails varchar(500),
activationid varchar(50),
validationMessage varchar(500),
lastValidated TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
||
-- organizations definition
CREATE TABLE IF NOT EXISTS organizations (
id VARCHAR(50) PRIMARY KEY,
name varchar(100) NOT NULL,
created_at INTEGER NOT NULL,
is_anonymous INTEGER NOT NULL DEFAULT 0 CHECK(is_anonymous IN (0,1)),
has_opted_updates INTEGER NOT NULL DEFAULT 1 CHECK(has_opted_updates IN (0,1))
);
||
-- sites definition
CREATE TABLE IF NOT EXISTS sites(
uuid VARCHAR(50) PRIMARY KEY,
alias VARCHAR(180) DEFAULT 'PROD',
url VARCHAR(300),
createdAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
||
-- invites definition
CREATE TABLE IF NOT EXISTS invites (
id INTEGER PRIMARY KEY AUTO_INCREMENT,
name varchar(100) NOT NULL,
email varchar(100) NOT NULL UNIQUE,
token varchar(500) NOT NULL,
created_at INTEGER NOT NULL,
role TEXT NOT NULL,
org_id VARCHAR(50) NOT NULL,
FOREIGN KEY(org_id) REFERENCES organizations(id)
);
||
-- org_domains definition
CREATE TABLE IF NOT EXISTS org_domains(
id VARCHAR(50) PRIMARY KEY,
org_id VARCHAR(50) NOT NULL,
name VARCHAR(50) NOT NULL UNIQUE,
created_at INTEGER NOT NULL,
updated_at INTEGER,
data TEXT NOT NULL,
FOREIGN KEY(org_id) REFERENCES organizations(id)
);
||
-- users definition
CREATE TABLE IF NOT EXISTS users (
id VARCHAR(50) PRIMARY KEY,
name varchar(100) NOT NULL,
email varchar(100) NOT NULL UNIQUE,
password TEXT NOT NULL,
created_at INTEGER NOT NULL,
profile_picture_url TEXT,
group_id VARCHAR(50) NOT NULL,
org_id VARCHAR(50) NOT NULL,
FOREIGN KEY(group_id) REFERENCES group_info(id),
FOREIGN KEY(org_id) REFERENCES organizations(id)
);
||
-- personal_access_tokens definition
CREATE TABLE IF NOT EXISTS personal_access_tokens (
id INTEGER PRIMARY KEY AUTO_INCREMENT,
role TEXT NOT NULL,
user_id VARCHAR(50) NOT NULL,
token varchar(500) NOT NULL UNIQUE,
name varchar(100) NOT NULL,
created_at INTEGER NOT NULL,
expires_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL,
last_used INTEGER NOT NULL,
revoked BOOLEAN NOT NULL,
updated_by_user_id VARCHAR(50) NOT NULL,
FOREIGN KEY(user_id) REFERENCES users(id)
);
||
-- reset_password_request definition
CREATE TABLE IF NOT EXISTS reset_password_request (
id INTEGER PRIMARY KEY AUTO_INCREMENT,
user_id VARCHAR(50) NOT NULL,
token varchar(500) NOT NULL,
FOREIGN KEY(user_id) REFERENCES users(id)
);
||
-- user_flags definition
CREATE TABLE IF NOT EXISTS user_flags (
user_id VARCHAR(50) PRIMARY KEY,
flags TEXT,
FOREIGN KEY(user_id) REFERENCES users(id)
);

	`
	table_schemaArray := strings.Split(table_schema, "||")
	for _, schema := range table_schemaArray {
		_, err = db.Exec(schema)
		if err != nil {
			return nil, fmt.Errorf("Error in creating tables: %v", err.Error())
		}
	}

	//
	mds := &ModelDaoMysql{db: db}
	//
	ctx := context.Background()
	if err := mds.initializeOrgPreferences(ctx); err != nil {
		return nil, err
	}
	if err := mds.initializeRBAC(ctx); err != nil {
		return nil, err
	}

	return mds, nil

}

// DB returns database connection
func (mds *ModelDaoMysql) DB() *sqlx.DB {
	return mds.db
}

// initializeOrgPreferences initializes in-memory telemetry settings. It is planned to have
// multiple orgs in the system. In case of multiple orgs, there will be separate instance
// of in-memory telemetry for each of the org, having their own settings. As of now, we only
// have one org so this method relies on the settings of this org to initialize the telemetry etc.
// TODO(Ahsan): Make it multi-tenant when we move to a system with multiple orgs.
func (mds *ModelDaoMysql) initializeOrgPreferences(ctx context.Context) error {

	// set anonymous setting as default in case of any failures to fetch UserPreference in below section
	telemetry.GetInstance().SetTelemetryAnonymous(constants.DEFAULT_TELEMETRY_ANONYMOUS)

	orgs, apiError := mds.GetOrgs(ctx)
	if apiError != nil {
		return apiError.Err
	}

	if len(orgs) > 1 {
		return errors.Errorf("Found %d organizations, expected one or none.", len(orgs))
	}

	var org model.Organization
	if len(orgs) == 1 {
		org = orgs[0]
	}

	// set telemetry fields from userPreferences
	telemetry.GetInstance().SetDistinctId(org.Id)

	users, _ := mds.GetUsers(ctx)
	countUsers := len(users)
	telemetry.GetInstance().SetCountUsers(int8(countUsers))
	if countUsers > 0 {
		telemetry.GetInstance().SetCompanyDomain(users[countUsers-1].Email)
		telemetry.GetInstance().SetUserEmail(users[countUsers-1].Email)
	}

	return nil
}

// initializeRBAC creates the ADMIN, EDITOR and VIEWER groups if they are not present.
func (mds *ModelDaoMysql) initializeRBAC(ctx context.Context) error {
	f := func(groupName string) error {
		_, err := mds.createGroupIfNotPresent(ctx, groupName)
		return errors.Wrap(err, "Failed to create group")
	}

	if err := f(constants.AdminGroup); err != nil {
		return err
	}
	if err := f(constants.EditorGroup); err != nil {
		return err
	}
	if err := f(constants.ViewerGroup); err != nil {
		return err
	}

	return nil
}

func (mds *ModelDaoMysql) createGroupIfNotPresent(ctx context.Context,
	name string) (*model.Group, error) {

	group, err := mds.GetGroupByName(ctx, name)
	if err != nil {
		return nil, errors.Wrap(err.Err, "Failed to query for root group")
	}
	if group != nil {
		return group, nil
	}

	zap.L().Debug("group is not found, creating it", zap.String("group_name", name))
	group, cErr := mds.CreateGroup(ctx, &model.Group{Name: name})
	if cErr != nil {
		return nil, cErr.Err
	}
	return group, nil
}
