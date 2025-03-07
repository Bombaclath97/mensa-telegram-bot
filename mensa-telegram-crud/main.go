package main

import (
	"database/sql"
	"log"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

type User struct {
	TelegramID        int64   `json:"telegramId"`
	MensaEmail        string  `json:"mensaEmail"`
	MembershipEndDate *string `json:"membershipEndDate,omitempty"`
	FirstName         string  `json:"firstName"`
	LastName          string  `json:"lastName"`
	MemberNumber      int64   `json:"memberNumber"`
}

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
		membership_end_date DATE,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL
		member_number INTEGER NOT NULL
	);`

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

	r.Run()
}

func getMember(c *gin.Context) {
	var user User
	id := c.Param("id")
	err := db.QueryRow("SELECT * FROM users WHERE telegram_id=?", id).Scan(&user.TelegramID, &user.MensaEmail, &user.MembershipEndDate, &user.FirstName, &user.LastName, &user.MemberNumber)
	if err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	c.JSON(200, user)
}

func createMember(c *gin.Context) {
	var user User
	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	log.Println("received user:", user)

	_, err := db.Exec("INSERT INTO users (telegram_id, mensa_email, membership_end_date, first_name, last_name, member_number) VALUES (?, ?, ?, ?, ?, ?)", user.TelegramID, user.MensaEmail, user.MembershipEndDate, user.FirstName, user.LastName, user.MemberNumber)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(201, user)
}

func updateMember(c *gin.Context) {
	var user User
	id := c.Param("id")
	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Exec("UPDATE users SET mensa_email=?, membership_end_date=?, first_name=?, last_name=?, member_number=? WHERE telegram_id=?", user.MensaEmail, user.MembershipEndDate, user.FirstName, user.LastName, user.MemberNumber, id)
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
	var users []User
	email := c.Param("email")
	rows, err := db.Query("SELECT * FROM users WHERE mensa_email LIKE ?", email+"%")
	if err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	for rows.Next() {
		var user User
		log.Println(rows)
		err = rows.Scan(&user.TelegramID, &user.MensaEmail, &user.MembershipEndDate, &user.FirstName, &user.LastName)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		} else {
			users = append(users, user)
		}
	}

	log.Println("found users:", users)
	c.JSON(200, users)
}
