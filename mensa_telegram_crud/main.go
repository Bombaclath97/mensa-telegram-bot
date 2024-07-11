package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

var db *sql.DB

func main() {
	godotenv.Load()
	db = connectDB()
	defer db.Close()

	router := mux.NewRouter()

	router.HandleFunc("/members/{id}", getMember).Methods("GET")
	router.HandleFunc("/members", addMember).Methods("POST")
	router.HandleFunc("/members/{id}", updateMember).Methods("PUT")

	log.Fatal(http.ListenAndServe(":8000", router))
}

func connectDB() *sql.DB {
	username := os.Getenv("DB_USERNAME")
	password := os.Getenv("DB_PASSWORD")
	url := os.Getenv("DB_URL")
	port := os.Getenv("DB_PORT")

	log.Printf("%s:%s@tcp(%s:%s)/mensa_telegram", username, password, url, port)
	connectionString := fmt.Sprintf("%s:%s@tcp(%s:%s)/mensa_telegram", username, password, url, port)

	db, err := sql.Open("mysql", connectionString)
	if err != nil {
		log.Fatal(err)
	}
	return db
}

func getMember(w http.ResponseWriter, r *http.Request) {
	variable := mux.Vars(r)
	memberID := variable["id"]

	log.Println("Received GET /members/{id} for Telegram ID", memberID)

	row := db.QueryRow("SELECT * FROM member_list WHERE telegram_id = ?", memberID)

	var member_tmp Member
	var tmp string

	if row.Scan(&member_tmp.TelegramID, &member_tmp.MensaEmail, &member_tmp.MensaNumber, &tmp, &member_tmp.FirstName, &member_tmp.LastName) == sql.ErrNoRows {
		log.Println("Telegram ID", memberID, "not found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	member_tmp.MensaMembershipEndDate, _ = time.Parse("2006-01-02", tmp)

	response, _ := json.Marshal(member_tmp)

	w.Header().Add("Content-Type", "application/json")
	w.Write(response)

	log.Println("Completed serving the request")
}

func addMember(w http.ResponseWriter, r *http.Request) {
	log.Println("Received POST /members")

	b, _ := io.ReadAll(r.Body)
	var member_tmp Member

	err := json.Unmarshal(b, &member_tmp)

	if err != nil {
		log.Println("Error in unmarshalling request body:", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	_, err = db.Exec("INSERT INTO member_list (telegram_id, mensa_email, mensa_number, mensa_membership_end_date, mensa_first_name, mensa_last_name) VALUES (?, ?, ?, ?, ?, ?)",
		member_tmp.TelegramID,
		member_tmp.MensaEmail,
		member_tmp.MensaNumber,
		member_tmp.MensaMembershipEndDate,
		member_tmp.FirstName,
		member_tmp.LastName)

	if err != nil {
		log.Println("Error in INSERT INTO statement:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func updateMember(w http.ResponseWriter, r *http.Request) {
	panic("Unimplemented")
}

type Member struct {
	TelegramID             uint      `json:"telegramId"`
	MensaEmail             string    `json:"mensaEmail"`
	MensaNumber            uint16    `json:"mensaNumber"`
	MensaMembershipEndDate time.Time `json:"membershipEndDate"`
	FirstName              string    `json:"firstName"`
	LastName               string    `json:"lastName"`
}
