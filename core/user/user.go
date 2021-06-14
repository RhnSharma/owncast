package user

import (
	"database/sql"
	"math/rand"
	"sync"
	"time"

	"github.com/owncast/owncast/core/data"
	"github.com/owncast/owncast/utils"
	"github.com/teris-io/shortid"

	log "github.com/sirupsen/logrus"
)

var _db *sql.DB
var _dbLock sync.Mutex

type User struct {
	Id              string                  `json:"id"`
	AccessToken     string                  `json:"-"`
	DisplayName     string                  `json:"displayName"`
	DisplayColor    int                     `json:"displayColor"`
	CreatedAt       time.Time               `json:"createdAt"`
	UsernameHistory []*usernameHistoryEntry `json:"usernameHistory"`
	DisabledAt      *time.Time              `json:"-"`
}

func (u *User) IsEnabled() bool {
	return u.DisabledAt == nil
}

func SetupUsers() {
	_db = data.GetDatabase()
	createUsersTable()
	createUsernameHistoryTable()
}

func createUsersTable() {
	log.Traceln("Creating users table...")

	createTableSQL := `CREATE TABLE IF NOT EXISTS users (
		"id" TEXT PRIMARY KEY,
		"access_token" string NOT NULL,
		"display_name" TEXT NOT NULL,
		"display_color" NUMBER NOT NULL,
		"created_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
		"name_changed_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
		"disabled_at" DATETIME 
	);`

	if err := execSQL(createTableSQL); err != nil {
		log.Fatalln(err)
	}
}

func CreateAnonymousUser(username string) (error, *User) {
	_dbLock.Lock()
	defer _dbLock.Unlock()

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
	addNameHistory(id, displayName)

	return nil, user
}

func ChangeUsername(userId string, username string) {
	addNameHistory(userId, username)
}

func create(user *User) error {
	tx, err := _db.Begin()
	if err != nil {
		panic(err)
	}
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
	tx, err := _db.Begin()
	if err != nil {
		log.Fatal(err)
		return err
	}

	var disabledAt *time.Time
	if !enabled {
		now := time.Now()
		disabledAt = &now
	} else {
		disabledAt = nil
	}

	stmt, err := tx.Prepare("UPDATE users SET disabled_at=? WHERE id IS ?")
	defer stmt.Close()

	if _, err := stmt.Exec(userID, disabledAt); err != nil {
		log.Fatal(err)
		return err
	}

	return nil
}

// GetUserByToken will return a user by an access token.
func GetUserByToken(token string) *User {
	if user := getCachedAccessTokenUser(token); user != nil {
		return user
	}

	// JOIN w/ user history table
	query := "SELECT id, display_name, display_color, created_at, disabled_at FROM users WHERE access_token = ?"
	row := _db.QueryRow(query, token)
	if row == nil || row.Scan().Error() != "" {
		return nil
	}

	return getUserFromRow(row)
}

// GetUserById will return a user by a user ID.
func GetUserById(id string) *User {
	if user := getCachedIdUser(id); user != nil {
		return user
	}

	// JOIN w/ user history table
	query := "SELECT id, display_name, display_color, created_at, disabled_at FROM users WHERE id = ?"
	row := _db.QueryRow(query, id)
	if row == nil {
		log.Errorln(row)
		return nil
	}
	return getUserFromRow(row)
}

func getUserFromRow(row *sql.Row) *User {
	var id string
	var displayName string
	var displayColor int
	var createdAt time.Time
	var disabledAt *time.Time

	if err := row.Scan(&id, &displayName, &displayColor, &createdAt, &disabledAt); err != nil {
		log.Fatalln(err)
		return nil
	}

	return &User{
		Id:              id,
		DisplayName:     displayName,
		DisplayColor:    displayColor,
		CreatedAt:       createdAt,
		DisabledAt:      disabledAt,
		UsernameHistory: getUsernameHistory(id),
	}
}