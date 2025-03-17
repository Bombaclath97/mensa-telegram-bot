package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/Bombaclath97/bomba-go-utils/logger"
	"github.com/gin-gonic/gin"
	_ "github.com/mutecomm/go-sqlcipher"

	model "git.bombaclath.cc/bombadurelli/mensa-bot-telegram/mensa-shared-models"
)

var db *sql.DB

var log = logger.Configure("mensa-telegram-crud")

func initDB() {
	var err error
	key := os.Getenv("DB_KEY")
	db, err = sql.Open("sqlite3", fmt.Sprintf("./mensa-telegram.db?_pragma_key=x'%s'&_pragma_cipher_page_size=4096", key))
	if err != nil {
		log.Fatal(err)
	}

	stmt := `
	CREATE TABLE IF NOT EXISTS users (
		telegram_id INTEGER PRIMARY KEY,
		mensa_email TEXT NOT NULL,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		member_number INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS groups (
	    user_id INTEGER NOT NULL,
		group_id INTEGER NOT NULL,
	    FOREIGN KEY(user_id) REFERENCES users(telegram_id) ON DELETE CASCADE ON UPDATE CASCADE
	);
	`

	_, err = db.Exec(stmt)
	if err != nil {
		log.Fatal(err)
	} else {
		log.Println("Database initialized")
	}

	db.Exec("PRAGMA foreign_keys=ON")
}

func main() {
	initDB()
	defer db.Close()

	r := gin.Default()
	r.GET("/members/:id", getMember)
	r.POST("/members", createMember)
	r.PUT("/members/:id", updateMember)
	r.DELETE("/members/:id", deleteMember)
	r.GET("/members", getAllMembers)

	r.GET("/members/email/:email", getMemberByEmail)

	r.GET("/groups/:id", getAllGroups)
	r.GET("/groups/:id/:group", getIsMemberInGroup)
	r.POST("/groups", createGroupAssociation)
	r.DELETE("/groups/:id", deleteAllGroup)

	r.Run()
}

func getMember(c *gin.Context) {
	var user model.User
	id := c.Param("id")
	err := db.QueryRow("SELECT * FROM users WHERE telegram_id=?", id).Scan(&user.TelegramID, &user.MensaEmail, &user.FirstName, &user.LastName, &user.MemberNumber)
	if err != nil {
		log.Printf("[GET /members/:id] User not found: %v", err)
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	log.Printf("[GET /members/:id] User found: %v", user)
	c.JSON(200, user)
}

func getAllMembers(c *gin.Context) {
	var users []model.User

	rows, err := db.Query("SELECT * FROM users")
	if err != nil {
		log.Printf("[GET /members] Error getting users: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var user model.User
		err := rows.Scan(&user.TelegramID, &user.MensaEmail, &user.FirstName, &user.LastName, &user.MemberNumber)
		if err != nil {
			log.Printf("[GET /members] Error scanning users: %v", err)
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		users = append(users, user)
	}

	log.Printf("[GET /members] Users found: %v", users)
	c.JSON(200, users)
}

func createMember(c *gin.Context) {
	var user model.User
	if err := c.ShouldBindJSON(&user); err != nil {
		log.Printf("[POST /members] Body not valid: %v", err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Exec("INSERT INTO users (telegram_id, mensa_email, first_name, last_name, member_number) VALUES (?, ?, ?, ?, ?)", user.TelegramID, user.MensaEmail, user.FirstName, user.LastName, user.MemberNumber)
	if err != nil {
		log.Printf("[POST /members] Error inserting user: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[POST /members] User created successfully: %v", user)
	c.JSON(201, user)
}

func updateMember(c *gin.Context) {
	var user model.User
	id := c.Param("id")
	if err := c.ShouldBindJSON(&user); err != nil {
		log.Printf("[PUT /members/:id] Body not valid: %v", err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Exec("UPDATE users SET mensa_email=?, first_name=?, last_name=?, member_number=? WHERE telegram_id=?", user.MensaEmail, user.FirstName, user.LastName, user.MemberNumber, id)
	if err != nil {
		log.Printf("[PUT /members/:id] Error updating user: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[PUT /members/:id] User updated successfully: %v", user)
	c.JSON(200, user)
}

func deleteMember(c *gin.Context) {
	id := c.Param("id")
	_, err := db.Exec("DELETE FROM users WHERE telegram_id=?", id)
	if err != nil {
		log.Printf("[DELETE /members/:id] Error deleting user: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[DELETE /members/:id] User deleted")
	c.JSON(200, gin.H{"message": "User deleted"})
}

func getMemberByEmail(c *gin.Context) {
	var user model.User
	email := c.Param("email")
	err := db.QueryRow("SELECT * FROM users WHERE mensa_email=?", email).Scan(&user.TelegramID, &user.MensaEmail, &user.FirstName, &user.LastName, &user.MemberNumber)
	if err != nil {
		log.Printf("[GET /members/email/:email] User not found: %v", err)
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	log.Printf("[GET /members/email/:email] User found: %v", user)
	c.JSON(200, user)
}

func getAllGroups(c *gin.Context) {
	var groups []model.Group
	id := c.Param("id")

	rows, err := db.Query("SELECT group_id FROM groups WHERE user_id=?", id)
	if err != nil {
		log.Printf("[GET /groups/:id] Error getting groups: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var group model.Group
		err := rows.Scan(&group.GroupID)
		if err != nil {
			log.Printf("[GET /groups/:id] Error scanning groups: %v", err)
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		groups = append(groups, group)
	}

	log.Printf("[GET /groups/:id] Groups found: %v", groups)
	c.JSON(200, groups)
}

func getIsMemberInGroup(c *gin.Context) {
	id := c.Param("id")
	group := c.Param("group")

	rows, err := db.Query("SELECT * FROM groups WHERE user_id=? AND group_id=?", id, group)
	if err != nil {
		log.Printf("[GET /groups/:id/:group] Error getting information about membership: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	if rows.Next() {
		log.Printf("[GET /groups/:id/:group] User is member of group")
		c.JSON(200, gin.H{"isMember": true})
		return
	} else {
		log.Printf("[GET /groups/:id/:group] User is not member of group")
		c.JSON(404, gin.H{"isMember": false})
		return
	}
}

func createGroupAssociation(c *gin.Context) {
	var group model.Group
	if err := c.ShouldBindJSON(&group); err != nil {
		log.Printf("[POST /groups] Body not valid: %v", err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Exec("INSERT INTO groups (user_id, group_id) VALUES (?, ?)", group.UserID, group.GroupID)
	if err != nil {
		log.Printf("[POST /groups] Error inserting group association: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[POST /groups] Group association created successfully: %v", group)
	c.JSON(201, group)
}

func deleteAllGroup(c *gin.Context) {
	id := c.Param("id")
	_, err := db.Exec("DELETE FROM groups WHERE user_id=?", id)
	if err != nil {
		log.Printf("[DELETE /groups/:id] Error deleting groups associated with user %s: %v", id, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[DELETE /groups/:id] Groups deleted for user %s", id)
	c.JSON(200, gin.H{"message": "Groups deleted"})
}
