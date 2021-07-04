package user

import (
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"github.com/owncast/owncast/core/data"
	"github.com/owncast/owncast/utils"
	"github.com/teris-io/shortid"

	log "github.com/sirupsen/logrus"
)

var _datastore *data.Datastore

type User struct {
	Id           string     `json:"id"`
	AccessToken  string     `json:"-"`
	DisplayName  string     `json:"displayName"`
	DisplayColor int        `json:"displayColor"`
	CreatedAt    time.Time  `json:"createdAt"`
	DisabledAt   *time.Time `json:"disabledAt,omitempty"`
}

func (u *User) IsEnabled() bool {
	return u.DisabledAt == nil
}

func SetupUsers() {
	_datastore = data.GetDatastore()
	createUsersTable()
}

func createUsersTable() {
	log.Traceln("Creating users table...")

	createTableSQL := `CREATE TABLE IF NOT EXISTS users (
		"id" TEXT PRIMARY KEY,
		"access_token" string NOT NULL,
		"display_name" TEXT NOT NULL,
		"display_color" NUMBER NOT NULL,
		"created_at" TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		"disabled_at" TIMESTAMP
	);`

	if err := execSQL(createTableSQL); err != nil {
		log.Fatalln(err)
	}
}

func CreateAnonymousUser(username string) (error, *User) {
	id := shortid.MustGenerate()
	accessToken, err := utils.GenerateAccessToken()
	if err != nil {
		log.Errorln("Unable to create access token for new user")
		return err, nil
	}

	var displayName = username
	if displayName == "" {
		displayName = utils.GeneratePhrase()
	}

	// Generate a random hue value. The UI should determine the right saturation and
	// lightness in order to make it look right.
	rangeLower := 0
	rangeUpper := 360
	displayColor := rangeLower + rand.Intn(rangeUpper-rangeLower+1)

	user := &User{
		Id:           id,
		AccessToken:  accessToken,
		DisplayName:  displayName,
		DisplayColor: displayColor,
		CreatedAt:    time.Now(),
	}

	if err := create(user); err != nil {
		return err, nil
	}

	setCachedIdUser(id, user)
	setCachedAccessTokenUser(accessToken, user)
	// addNameHistory(id, displayName)

	return nil, user
}

func ChangeUsername(userId string, username string) {
	_datastore.DbLock.Lock()
	defer _datastore.DbLock.Unlock()

	tx, err := _datastore.DB.Begin()

	if err != nil {
		panic(err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE users SET display_name = ? WHERE id = ?")

	if err != nil {
		panic(err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(username, userId)
	if err != nil {
		panic(err)
	}

	if err := tx.Commit(); err != nil {
		log.Errorln("error changing display name of user", userId, err)
	}

	// addNameHistory(userId, username)
}

func create(user *User) error {
	_datastore.DbLock.Lock()
	defer _datastore.DbLock.Unlock()

	tx, err := _datastore.DB.Begin()

	if err != nil {
		panic(err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO users(id, access_token, display_name, display_color, created_at) values(?, ?, ?, ?, ?)")

	if err != nil {
		panic(err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(user.Id, user.AccessToken, user.DisplayName, user.DisplayColor, user.CreatedAt)
	if err != nil {
		panic(err)
	}

	return tx.Commit()
}

func SetEnabled(userID string, enabled bool) error {
	_datastore.DbLock.Lock()
	defer _datastore.DbLock.Unlock()

	tx, err := _datastore.DB.Begin()

	if err != nil {
		log.Fatal(err)
		return err
	}
	defer tx.Rollback()

	var stmt *sql.Stmt
	if !enabled {
		stmt, err = tx.Prepare("UPDATE users SET disabled_at=DATETIME('now', 'localtime') WHERE id IS ?")
	} else {
		stmt, err = tx.Prepare("UPDATE users SET disabled_at=null WHERE id IS ?")
	}

	if err != nil {
		log.Fatal(err)
	}

	defer stmt.Close()

	if _, err := stmt.Exec(userID); err != nil {
		log.Fatal(err)
		return err
	}

	return tx.Commit()
}

// GetUserByToken will return a user by an access token.
func GetUserByToken(token string) *User {
	if user := getCachedAccessTokenUser(token); user != nil {
		return user
	}

	_datastore.DbLock.Lock()
	defer _datastore.DbLock.Unlock()

	query := "SELECT id, display_name, display_color, created_at, disabled_at FROM users WHERE access_token = ?"
	fmt.Println("SELECT id, display_name, display_color, created_at, disabled_at FROM users WHERE access_token =", token)
	row := _datastore.DB.QueryRow(query, token)

	return getUserFromRow(row)
}

// GetUserById will return a user by a user ID.
func GetUserById(id string) *User {
	if user := getCachedIdUser(id); user != nil {
		return user
	}

	_datastore.DbLock.Lock()
	defer _datastore.DbLock.Unlock()

	query := "SELECT id, display_name, display_color, created_at, disabled_at FROM users WHERE id = ?"
	row := _datastore.DB.QueryRow(query, id)
	if row == nil {
		log.Errorln(row)
		return nil
	}
	return getUserFromRow(row)
}

// GetDisabledUsers will return back all the currently disabled users.
func GetDisabledUsers() []*User {
	query := "SELECT id, display_name, display_color, created_at, disabled_at FROM users WHERE disabled_at IS NOT NULL"

	rows, err := _datastore.DB.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	users := getUsersFromRows(rows)
	return users
}

func getUsersFromRows(rows *sql.Rows) []*User {
	users := make([]*User, 0)

	for rows.Next() {
		var id string
		var displayName string
		var displayColor int
		var createdAt time.Time
		var disabledAt *time.Time

		if err := rows.Scan(&id, &displayName, &displayColor, &createdAt, &disabledAt); err != nil {
			log.Errorln("error creating collection of users from results", err)
			return nil
		}

		user := &User{
			Id:           id,
			DisplayName:  displayName,
			DisplayColor: displayColor,
			CreatedAt:    createdAt,
			DisabledAt:   disabledAt,
			// UsernameHistory: GetUsernameHistory(id),
		}
		users = append(users, user)
	}

	return users
}

func getUserFromRow(row *sql.Row) *User {
	var id string
	var displayName string
	var displayColor int
	var createdAt time.Time
	var disabledAt *time.Time

	if err := row.Scan(&id, &displayName, &displayColor, &createdAt, &disabledAt); err != nil {
		log.Errorln("error fetching single user from row", err)
		return nil
	}

	return &User{
		Id:           id,
		DisplayName:  displayName,
		DisplayColor: displayColor,
		CreatedAt:    createdAt,
		DisabledAt:   disabledAt,
		// UsernameHistory: GetUsernameHistory(id),
	}
}
