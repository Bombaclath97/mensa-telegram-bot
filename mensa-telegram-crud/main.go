package main

import (
	"database/sql"
	"log"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	model "git.bombaclath.cc/bombadurelli/mensa-bot-telegram/mensa-shared-models"
)

var db *sql.DB

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./mensa-telegram.db")
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
}

func main() {
	initDB()
	defer db.Close()

	r := gin.Default()
	r.GET("/members/:id", getMember)
	r.POST("/members", createMember)
	r.PUT("/members/:id", updateMember)
	r.DELETE("/members/:id", deleteMember)

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
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	c.JSON(200, user)
}

func createMember(c *gin.Context) {
	var user model.User
	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	log.Println("received user:", user)

	_, err := db.Exec("INSERT INTO users (telegram_id, mensa_email, first_name, last_name, member_number) VALUES (?, ?, ?, ?, ?)", user.TelegramID, user.MensaEmail, user.FirstName, user.LastName, user.MemberNumber)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(201, user)
}

func updateMember(c *gin.Context) {
	var user model.User
	id := c.Param("id")
	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Exec("UPDATE users SET mensa_email=?, first_name=?, last_name=?, member_number=? WHERE telegram_id=?", user.MensaEmail, user.FirstName, user.LastName, user.MemberNumber, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, user)
}

func deleteMember(c *gin.Context) {
	id := c.Param("id")
	_, err := db.Exec("DELETE FROM users WHERE telegram_id=?", id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "User deleted"})
}

func getMemberByEmail(c *gin.Context) {
	var user model.User
	email := c.Param("email")
	err := db.QueryRow("SELECT * FROM users WHERE mensa_email=?", email).Scan(&user.TelegramID, &user.MensaEmail, &user.FirstName, &user.LastName, &user.MemberNumber)
	if err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	c.JSON(200, user)
}

func getAllGroups(c *gin.Context) {
	var groups []model.Group
	id := c.Param("id")

	rows, err := db.Query("SELECT group_id FROM groups WHERE user_id=?", id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var group model.Group
		err := rows.Scan(&group.GroupID)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		groups = append(groups, group)
	}

	c.JSON(200, groups)
}

func getIsMemberInGroup(c *gin.Context) {
	id := c.Param("id")
	group := c.Param("group")

	rows, err := db.Query("SELECT * FROM groups WHERE user_id=? AND group_id=?", id, group)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	if rows.Next() {
		c.JSON(200, gin.H{"isMember": true})
		return
	} else {
		c.JSON(404, gin.H{"isMember": false})
		return
	}
}

func createGroupAssociation(c *gin.Context) {
	var group model.Group
	if err := c.ShouldBindJSON(&group); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Exec("INSERT INTO groups (user_id, group_id) VALUES (?, ?)", group.UserID, group.GroupID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(201, group)
}

func deleteAllGroup(c *gin.Context) {
	id := c.Param("id")
	_, err := db.Exec("DELETE FROM groups WHERE user_id=?", id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "Groups deleted"})
}
